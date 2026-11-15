package handler

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
)

func pwaRouter(t *testing.T) http.Handler {
	t.Helper()
	t.Chdir("../..")
	return NewRouter(&Deps{Session: scs.New()})
}

func requestPWA(t *testing.T, router http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d", method, path, w.Code)
	}
	return w
}

func TestPWARootAssetsHaveCorrectScopeAndHeaders(t *testing.T) {
	router := pwaRouter(t)
	for path, contentType := range map[string]string{
		"/sw.js": "text/javascript", "/manifest.webmanifest": "application/manifest+json",
		"/favicon.svg": "image/svg+xml", "/favicon.ico": "image/x-icon",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := requestPWA(t, router, method, path)
			if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, contentType) {
				t.Errorf("%s MIME = %q, want %s", path, got, contentType)
			}
			if got := w.Header().Get("Cache-Control"); got != "no-cache" {
				t.Errorf("%s Cache-Control = %q", path, got)
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("%s missing nosniff", path)
			}
			if path == "/sw.js" && w.Header().Get("Service-Worker-Allowed") != "/" {
				t.Error("worker must allow origin-root scope")
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Errorf("HEAD %s returned a body", path)
			}
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sw.js", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /sw.js status = %d, want 405", w.Code)
	}
}

func TestPWAManifestAndDeclaredIconsAreServed(t *testing.T) {
	router := pwaRouter(t)
	w := requestPWA(t, router, http.MethodGet, "/manifest.webmanifest")
	var manifest struct {
		ID, Name, ShortName, StartURL, Scope, Display, ThemeColor, BackgroundColor string
		Icons                                                                      []struct{ Src, Sizes, Type, Purpose string }
	}
	// JSON names with underscores are explicit to validate the real manifest contract.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"id": "/v2/login", "name": "유하다 헤어", "short_name": "유하다 헤어",
		"start_url": "/v2/login", "scope": "/", "display": "standalone",
		"theme_color": "#F0E4D0", "background_color": "#FAF5EC",
	} {
		var got string
		if err := json.Unmarshal(raw[key], &got); err != nil || got != want {
			t.Errorf("manifest %s = %q, want %q (err=%v)", key, got, want, err)
		}
	}
	wantIcons := map[string]bool{"192x192/any": false, "512x512/any": false, "512x512/maskable": false}
	for _, icon := range manifest.Icons {
		key := icon.Sizes + "/" + icon.Purpose
		if _, ok := wantIcons[key]; !ok || icon.Type != "image/png" || !strings.HasPrefix(icon.Src, "/static/icons/") {
			t.Fatalf("unexpected manifest icon: %+v", icon)
		}
		wantIcons[key] = true
		w := requestPWA(t, router, http.MethodGet, icon.Src)
		if !strings.HasPrefix(w.Header().Get("Content-Type"), "image/png") {
			t.Errorf("%s MIME = %s", icon.Src, w.Header().Get("Content-Type"))
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if (icon.Sizes == "192x192" && (config.Width != 192 || config.Height != 192)) ||
			(icon.Sizes == "512x512" && (config.Width != 512 || config.Height != 512)) {
			t.Errorf("%s dimensions = %dx%d", icon.Src, config.Width, config.Height)
		}
	}
	for key, present := range wantIcons {
		if !present {
			t.Errorf("manifest missing %s icon", key)
		}
	}
	apple := requestPWA(t, router, http.MethodGet, "/static/icons/apple-touch-icon.png")
	config, _, err := image.DecodeConfig(apple.Body)
	if err != nil || config.Width != 180 || config.Height != 180 {
		t.Fatalf("Apple icon dimensions invalid: %+v, %v", config, err)
	}
	offline := requestPWA(t, router, http.MethodGet, "/static/pwa/offline.html")
	if !strings.HasPrefix(offline.Header().Get("Content-Type"), "text/html") || !strings.Contains(offline.Body.String(), "인터넷 연결을 확인") {
		t.Error("offline guidance missing or wrong MIME")
	}
	registration := requestPWA(t, router, http.MethodGet, "/static/pwa/register.js")
	contents, err := io.ReadAll(registration.Body)
	if err != nil || !bytes.Contains(contents, []byte("register('/sw.js', { scope: '/', updateViaCache: 'none' })")) {
		t.Error("registration must use root scope and bypass script HTTP cache")
	}
}
