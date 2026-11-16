package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
)

func TestLegacyAdminAddressesMoveToV2(t *testing.T) {
	router := NewRouter(&Deps{Session: scs.New(), RedirectLegacyAdmin: true})
	for path, want := range map[string]string{
		"/": "/v2/", "/admin": "/v2/admin", "/admin/": "/v2/admin",
		"/admin?period=month": "/v2/admin?period=month",
		"/admin/members":      "/v2/admin/members", "/admin/members/": "/v2/admin/members",
		"/admin/members?q=%EA%B3%A0%EA%B0%9D": "/v2/admin/members?q=%EA%B3%A0%EA%B0%9D",
		"/admin/members/new":                  "/v2/admin/members/new", "/admin/members/fixture": "/v2/admin/members/fixture",
		"/admin/cards": "/v2/admin/cards", "/admin/transactions?period=all&type=charge": "/v2/admin/transactions?period=all&type=charge",
		"/admin/members/fixture/charge/modal": "/v2/admin/members/fixture",
		"/admin/members/fixture/memo/edit":    "/v2/admin/members/fixture",
	} {
		for _, method := range []string{"GET", "HEAD"} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != 307 || w.Header().Get("Location") != want || w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s => %d %q", method, path, w.Code, w.Header().Get("Location"))
			}
		}
	}
}

func TestOldWritesAndHTMXNeverExecuteOrReplay(t *testing.T) {
	called := false
	router := legacyAdminRedirect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	for _, tc := range []struct{ method, path, target string }{
		{"POST", "/admin/members/fixture/charge", "/v2/admin/members/fixture"},
		{"POST", "/admin/members/fixture/deduct", "/v2/admin/members/fixture"},
		{"PATCH", "/admin/members/fixture/memo", "/v2/admin/members/fixture"},
		{"DELETE", "/admin/members/fixture", "/v2/admin/members/fixture"},
		{"POST", "/admin/members", "/v2/admin/members"},
		{"POST", "/admin/members/fixture/card", "/v2/admin/members/fixture"},
		{"POST", "/admin/members/fixture/lost", "/v2/admin/members/fixture"},
		{"POST", "/admin/members/fixture/reactivate", "/v2/admin/members/fixture"},
	} {
		for _, mode := range []string{"json", "html", "htmx"} {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader("amount=50000"))
			if mode == "json" {
				r.Header.Set("Accept", "application/json")
			}
			if mode == "htmx" {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if called {
				t.Fatal("old business handler executed")
			}
			if mode == "htmx" {
				if w.Code != 204 || w.Header().Get("HX-Redirect") != tc.target {
					t.Fatal("HTMX must navigate to new read page")
				}
				continue
			}
			if w.Code != 409 || w.Header().Get("Location") != tc.target {
				t.Errorf("stale %s %s got%d %q", tc.method, mode, w.Code, w.Header().Get("Location"))
			}
			if mode == "json" {
				var body struct {
					OK       bool
					Redirect string
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.OK || body.Redirect != tc.target {
					t.Fatal("write returned misleading success")
				}
			}
		}
	}
	r := httptest.NewRequest("GET", "/admin/members/fixture/charge/modal", nil)
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("HX-Redirect") != "/v2/admin/members/fixture" {
		t.Fatal("old modal left v1 JavaScript active")
	}
}

func TestLegacyRedirectDoesNotCaptureCustomerStaticOrOtherPaths(t *testing.T) {
	for _, path := range []string{"/m/fictional-card", "/static/css/app.css", "/sw.js", "/manifest.webmanifest", "/favicon.svg", "/healthz", "/logout", "/v2/admin", "/administer"} {
		called := false
		router := legacyAdminRedirect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(299) }))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if !called || w.Code != 299 {
			t.Errorf("captured unrelated %s", path)
		}
	}
	for _, path := range []string{"/admin/../login", "/admin/%2e%2e/login", "/admin/\\evil"} {
		w := httptest.NewRecorder()
		legacyAdminRedirect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unsafe path forwarded") })).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Header().Get("Location") != "/v2/admin" {
			t.Errorf("unsafe destination %q", w.Header().Get("Location"))
		}
	}
}

func TestDisablingV2UIKeepsLegacyRollbackAvailable(t *testing.T) {
	router := NewRouter(&Deps{Session: scs.New(), RedirectLegacyAdmin: false})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/admin", nil))
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/login?") {
		t.Fatalf("legacy fallback no longer reachable: %d %q", w.Code, w.Header().Get("Location"))
	}
}
