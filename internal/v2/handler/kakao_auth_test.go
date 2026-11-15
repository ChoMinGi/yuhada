package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	v2auth "github.com/mingicho/yuhada/internal/v2/auth"
	v2kakao "github.com/mingicho/yuhada/internal/v2/kakao"
)

type kakaoRoundTripFunc func(*http.Request) (*http.Response, error)

func (f kakaoRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestKakaoCallbackAuthorization(t *testing.T) {
	verified := func(email string) map[string]any {
		return map[string]any{"email": email, "email_needs_agreement": false, "is_email_valid": true, "is_email_verified": true}
	}
	for _, tc := range []struct {
		name, whitelist, legacyID string
		account                   map[string]any
		status                    int
	}{
		{"verified email", "owner@example.com", "", verified("owner@example.com"), 303},
		{"case and whitespace", "  OWNER@EXAMPLE.COM  ", "", verified(" owner@example.com "), 303},
		{"multiple addresses", "other@example.com, OWNER@example.com", "", verified("owner@example.com"), 303},
		{"unlisted", "owner@example.com", "", verified("other@example.com"), 403},
		{"missing email", "owner@example.com", "", verified(""), 403},
		{"missing account", "owner@example.com", "", nil, 403},
		{"substring", "owner@example.com", "", verified("xowner@example.com"), 403},
		{"plus alias", "owner@example.com", "", verified("owner+alias@example.com"), 403},
		{"domain only", "example.com", "", verified("owner@example.com"), 403},
		{"id cannot bypass", "owner@example.com", "42", verified("other@example.com"), 403},
		{"empty entries fail closed", ", ,", "42", verified("owner@example.com"), 403},
		{"legacy success", "", "42", nil, 303},
		{"legacy rejection", "", "99", nil, 403},
		{"legacy setup", "", "", nil, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runKakaoCallback(t, tc.whitelist, tc.legacyID, tc.account, "", tc.status)
		})
	}
	for _, flag := range []string{"email_needs_agreement", "is_email_valid", "is_email_verified"} {
		t.Run("wrong "+flag, func(t *testing.T) {
			a := verified("owner@example.com")
			a[flag] = flag == "email_needs_agreement"
			runKakaoCallback(t, "owner@example.com", "42", a, "", 403)
		})
		t.Run("omitted "+flag, func(t *testing.T) {
			a := verified("owner@example.com")
			delete(a, flag)
			runKakaoCallback(t, "owner@example.com", "42", a, "", 403)
		})
	}
}

func runKakaoCallback(t *testing.T, whitelist, legacyID string, account map[string]any, next string, wantStatus int, failRenewal ...bool) {
	t.Helper()
	sm := v2auth.NewSessionManager(nil, false)
	apiCalls := 0
	payload, err := json.Marshal(map[string]any{"id": 42, "kakao_account": account})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &v2kakao.Config{ClientID: "test-client", RedirectURI: "https://salon.example/v2/kakao/callback", RequireEmail: strings.TrimSpace(whitelist) != "", HTTPClient: &http.Client{Transport: kakaoRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		apiCalls++
		body := string(payload)
		if r.URL.Path == "/oauth/token" {
			body = `{"access_token":"test-token"}`
		} else if r.URL.Path != "/v2/user/me" {
			t.Fatalf("unexpected API %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	h := &PageHandlers{Deps: &Deps{Session: sm, Kakao: cfg, AdminKakaoEmails: whitelist, AdminKakaoID: legacyID}}
	mux := http.NewServeMux()
	mux.HandleFunc("/start", h.KakaoLogin)
	mux.HandleFunc("/callback", h.KakaoCallback)
	mux.HandleFunc("/probe", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, sm.GetString(r.Context(), v2auth.SessionKeyRole))
	})
	wrapped := sm.LoadAndSave(mux)
	start := httptest.NewRecorder()
	wrapped.ServeHTTP(start, httptest.NewRequest("GET", "/start?next="+url.QueryEscape(next), nil))
	if start.Code != 303 {
		t.Fatalf("start = %d", start.Code)
	}
	authURL, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	cb := httptest.NewRequest("GET", "/callback?code=test-code&state="+url.QueryEscape(authURL.Query().Get("state"))+"&next="+url.QueryEscape(next), nil)
	for _, cookie := range start.Result().Cookies() {
		cb.AddCookie(cookie)
	}
	if len(failRenewal) > 0 && failRenewal[0] {
		sm.Store = deleteFailStore{sm.Store}
	}
	result := httptest.NewRecorder()
	wrapped.ServeHTTP(result, cb)
	if result.Code != wantStatus {
		t.Fatalf("callback = %d, want %d: %s", result.Code, wantStatus, result.Body.String())
	}
	if apiCalls != 2 {
		t.Fatalf("API calls = %d", apiCalls)
	}
	if wantStatus == 303 {
		wantNext := "/v2/admin"
		if next == "/v2/admin/sales?period=month" {
			wantNext = next
		}
		if result.Header().Get("Location") != wantNext {
			t.Fatalf("unsafe/wrong redirect %q", result.Header().Get("Location"))
		}
	}
	probe := httptest.NewRequest("GET", "/probe", nil)
	cookies := result.Result().Cookies()
	if len(cookies) == 0 {
		cookies = start.Result().Cookies()
	}
	for _, cookie := range cookies {
		probe.AddCookie(cookie)
	}
	probed := httptest.NewRecorder()
	wrapped.ServeHTTP(probed, probe)
	if got := probed.Body.String(); (got == v2auth.RoleAdmin) != (wantStatus == 303) {
		t.Fatalf("role = %q after %d callback", got, result.Code)
	}
	if strings.Contains(result.Body.String(), "owner@example.com") {
		t.Fatal("callback exposed whitelist/email")
	}
}

type deleteFailStore struct{ scs.Store }

func (s deleteFailStore) Delete(string) error { return errors.New("session store unavailable") }

func TestKakaoCallbackSessionRenewalFailure(t *testing.T) {
	runKakaoCallback(t, "", "42", nil, "", 500, true)
}

func TestKakaoCallbackRejectsBadStateBeforeAPI(t *testing.T) {
	sm := v2auth.NewSessionManager(nil, false)
	h := &PageHandlers{Deps: &Deps{Session: sm, Kakao: &v2kakao.Config{HTTPClient: &http.Client{Transport: kakaoRoundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("API called for invalid state"); return nil, nil })}}}}
	for _, query := range []string{"code=x", "code=x&state=wrong"} {
		response := httptest.NewRecorder()
		sm.LoadAndSave(http.HandlerFunc(h.KakaoCallback)).ServeHTTP(response, httptest.NewRequest("GET", "/callback?"+query, nil))
		if response.Code != 400 {
			t.Fatalf("status = %d", response.Code)
		}
	}
}

func TestKakaoCallbackRejectsExternalNext(t *testing.T) {
	for _, next := range []string{"//evil.example/path", "/\\evil.example", "https://evil.example", "%2f%2fevil.example", "/\r\nLocation: evil"} {
		t.Run(next, func(t *testing.T) { runKakaoCallback(t, "", "42", nil, next, 303) })
	}
	runKakaoCallback(t, "", "42", nil, "/v2/admin/sales?period=month", 303)
}
