package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	auth1 "github.com/mingicho/yuhada/internal/auth"
	handler1 "github.com/mingicho/yuhada/internal/handler"
	service1 "github.com/mingicho/yuhada/internal/service"
	auth2 "github.com/mingicho/yuhada/internal/v2/auth"
	kakao2 "github.com/mingicho/yuhada/internal/v2/kakao"
)

type pinlessFixture struct {
	t      *testing.T
	client *http.Client
	deps   *Deps
}

func newPinlessFixture(t *testing.T, email string, authOnly bool) *pinlessFixture {
	t.Helper()
	db, h, _, _ := handlerLedgerFixture(t)
	s1, s2 := auth1.NewSessionManager(db, false), auth2.NewSessionManager(db, false)
	h.Deps.Legacy = service1.New(db, nil)
	h.Deps.LegacySession = s1
	h.Deps.Session = s2
	h.Deps.AuthOnly = authOnly
	h.Deps.AdminKakaoEmails = "owner@example.com"
	h.Deps.Kakao = &kakao2.Config{ClientID: "fixture", RedirectURI: "https://salon.example/v2/kakao/callback", RequireEmail: true, HTTPClient: &http.Client{Transport: kakaoRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"fixture-token"}`
		if r.URL.Path == "/v2/user/me" {
			b, _ := json.Marshal(map[string]any{"id": 42, "kakao_account": map[string]any{"email": email, "email_needs_agreement": false, "is_email_valid": true, "is_email_verified": true}})
			body = string(b)
		} else if r.URL.Path != "/oauth/token" {
			t.Fatalf("unexpected provider request %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	r := chi.NewRouter()
	r.Handle("/fixture/*", s1.LoadAndSave(s2.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fixture/old-pin" || r.URL.Path == "/fixture/old-kakao" {
			s1.Put(r.Context(), auth1.SessionKeyRole, auth1.RoleAdmin)
			s1.Put(r.Context(), auth1.SessionKeyAdminID, "old-pin-admin")
			s2.Put(r.Context(), auth2.SessionKeyRole, auth2.RoleAdmin)
			s2.Put(r.Context(), auth2.SessionKeyAdminID, "old-pin-admin")
			if r.URL.Path == "/fixture/old-kakao" {
				s2.Put(r.Context(), auth2.SessionKeyKakaoID, "42")
			}
			w.WriteHeader(204)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"legacy": auth1.IsAdmin(r.Context(), s1), "v2": auth2.IsAdmin(r.Context(), s2), "legacy_id": s1.GetString(r.Context(), auth1.SessionKeyAdminID), "v2_id": s2.GetString(r.Context(), auth2.SessionKeyAdminID)})
	}))))
	r.Mount("/v2", NewRouter(h.Deps))
	r.Mount("/", handler1.NewRouter(&handler1.Deps{Services: h.Deps.Legacy, Session: s1, KakaoSession: s2}))
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: kakaoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Result(), nil
	})}
	return &pinlessFixture{t: t, client: c, deps: h.Deps}
}

func (f *pinlessFixture) get(path string) (int, string, string) {
	f.t.Helper()
	r, err := f.client.Get("https://salon.example" + path)
	if err != nil {
		f.t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(b), r.Header.Get("Location")
}
func (f *pinlessFixture) signIn(next string) (int, string) {
	f.t.Helper()
	code, _, location := f.get("/v2/kakao/login?next=" + url.QueryEscape(next))
	if code != 303 {
		f.t.Fatalf("start %d", code)
	}
	u, _ := url.Parse(location)
	code, _, location = f.get("/v2/kakao/callback?code=fixture&state=" + url.QueryEscape(u.Query().Get("state")) + "&next=%2Fadmin%2Fcards")
	return code, location
}

func TestKakaoOnlyAuthenticatesBothFrontendsAndEitherLogoutRevokesBoth(t *testing.T) {
	for _, logout := range []string{"/logout", "/v2/logout"} {
		t.Run(logout, func(t *testing.T) {
			f := newPinlessFixture(t, "owner@example.com", false)
			if code, _, loc := f.get("/login"); code != 303 || loc != "/v2/login" {
				t.Fatalf("old login: %d %q", code, loc)
			}
			if code, body, _ := f.get("/v2/login"); code != 200 || strings.Contains(body, `name="pin"`) || strings.Contains(body, "PIN") || !strings.Contains(body, "카카오로 로그인") {
				t.Fatal("PIN-free login missing")
			}
			f.get("/fixture/old-pin")
			if code, loc := f.signIn("/admin/transactions"); code != 303 || loc != "/admin/transactions" {
				t.Fatalf("callback %d %q", code, loc)
			}
			for _, path := range []string{"/admin", "/v2/admin"} {
				if code, _, _ := f.get(path); code != 200 {
					t.Errorf("not authorized %s: %d", path, code)
				}
			}
			_, body, _ := f.get("/fixture/state")
			if !strings.Contains(body, `"legacy":true`) || !strings.Contains(body, `"v2":true`) || strings.Count(body, "kakao:42") != 2 {
				t.Fatalf("wrong principal %s", body)
			}
			r, err := f.client.PostForm("https://salon.example"+logout, url.Values{})
			if err != nil {
				t.Fatal(err)
			}
			r.Body.Close()
			if r.StatusCode != 303 {
				t.Fatal("logout failed")
			}
			for _, path := range []string{"/admin", "/v2/admin"} {
				if code, _, _ := f.get(path); code != 303 {
					t.Errorf("logout retained authorization %s: %d", path, code)
				}
			}
		})
	}
}

func TestPINOnlyCookiesDoNotAuthorizeOrLoop(t *testing.T) {
	f := newPinlessFixture(t, "owner@example.com", false)
	f.get("/fixture/old-pin")
	for _, path := range []string{"/admin", "/v2/admin", "/", "/v2/"} {
		if code, _, _ := f.get(path); code != 303 {
			t.Errorf("old PIN reached %s: %d", path, code)
		}
	}
	if code, _, location := f.get("/login"); code != 303 || location != "/v2/login" {
		t.Fatal("old login loop")
	}
	if code, body, _ := f.get("/v2/login"); code != 200 || strings.Contains(body, `name="pin"`) {
		t.Fatal("old PIN cookie blocks Kakao login")
	}
}

func TestExistingKakaoCookieRestoresLegacyBeforeLoginRedirect(t *testing.T) {
	f := newPinlessFixture(t, "owner@example.com", false)
	f.get("/fixture/old-kakao")
	if code, _, loc := f.get("/v2/login?next=%2Fadmin"); code != 303 || loc != "/admin" {
		t.Fatalf("bridge redirect %d %q", code, loc)
	}
	if code, _, _ := f.get("/admin"); code != 200 {
		t.Fatalf("legacy cookie not granted %d", code)
	}
	_, body, _ := f.get("/fixture/state")
	if strings.Count(body, "kakao:42") != 2 {
		t.Fatal("stale PIN actor retained")
	}
}

func TestKakaoAuthSurvivesDisablingNewFrontend(t *testing.T) {
	f := newPinlessFixture(t, "owner@example.com", true)
	if code, loc := f.signIn(""); code != 303 || loc != "/admin" {
		t.Fatalf("AuthOnly callback %d %q", code, loc)
	}
	if code, _, _ := f.get("/admin"); code != 200 {
		t.Fatal("legacy UI unavailable")
	}
	if code, _, _ := f.get("/v2/admin"); code != 404 {
		t.Fatal("new UI remains enabled")
	}
	if code, _, loc := f.get("/v2/login?next=%2Fv2%2Fadmin%2Fsales"); code != 303 || loc != "/admin" {
		t.Fatal("AuthOnly redirected to unavailable UI")
	}
}

func TestUnlistedKakaoCannotAuthorizeEitherFrontend(t *testing.T) {
	f := newPinlessFixture(t, "unlisted@example.com", false)
	f.get("/fixture/old-pin")
	if code, _ := f.signIn("/admin"); code != 403 {
		t.Fatal("unlisted email accepted")
	}
	_, body, _ := f.get("/fixture/state")
	if strings.Contains(body, `true`) {
		t.Fatalf("unlisted account obtained a role: %s", body)
	}
}

func TestNewKakaoAttemptClearsPreviousReturnPath(t *testing.T) {
	f := newPinlessFixture(t, "owner@example.com", false)
	f.get("/v2/kakao/login?next=%2Fv2%2Fadmin%2Fsales")
	if code, loc := f.signIn(""); code != 303 || loc != "/v2/admin" {
		t.Fatalf("stale or callback return path honored: %d %q", code, loc)
	}
}

func TestLegacySessionRenewalFailureDoesNotGrantKakaoRoles(t *testing.T) {
	f := newPinlessFixture(t, "owner@example.com", false)
	f.get("/fixture/old-pin")
	f.deps.LegacySession.Store = deleteFailStore{f.deps.LegacySession.Store}
	if code, _ := f.signIn("/admin"); code != 500 {
		t.Fatalf("failed legacy rotation returned %d", code)
	}
	_, body, _ := f.get("/fixture/state")
	if strings.Contains(body, `true`) {
		t.Fatalf("rotation failure granted a role: %s", body)
	}
}
