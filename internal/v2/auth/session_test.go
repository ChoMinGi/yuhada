package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSessionSurvivesManagerRestart(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE admin_sessions_v2(token TEXT PRIMARY KEY,data BLOB NOT NULL,expiry REAL NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	s1 := NewSessionManager(db, false)
	if s1.Lifetime != 30*24*time.Hour || s1.IdleTimeout != 12*time.Hour {
		t.Fatal("login lifetime differs from existing frontend")
	}
	w := httptest.NewRecorder()
	s1.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s1.Put(r.Context(), SessionKeyRole, RoleAdmin)
		w.WriteHeader(204)
	})).ServeHTTP(w, httptest.NewRequest("GET", "/v2/login", nil))
	s2 := NewSessionManager(db, false)
	r := httptest.NewRequest("GET", "/v2/admin", nil)
	for _, cookie := range w.Result().Cookies() {
		r.AddCookie(cookie)
	}
	result := httptest.NewRecorder()
	s2.LoadAndSave(RequireAdmin(s2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))).ServeHTTP(result, r)
	if result.Code != 200 {
		t.Fatalf("existing cookie lost after restart: %d", result.Code)
	}
}

func TestExpiredWalletRequestReturnsExplicitJSON(t *testing.T) {
	s := NewSessionManager(nil, false)
	for _, suffix := range []string{"charge", "deduct", "memo", "card", "lost", "reactivate"} {
		t.Run(suffix, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v2/admin/members/fictional/"+suffix+"?old=1", nil)
			r.Header.Set("Accept", "application/json")
			w := httptest.NewRecorder()
			s.LoadAndSave(RequireAdmin(s)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unauthenticated write was executed") }))).ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatalf("expired operation returned %d", w.Code)
			}
			var result struct {
				OK       bool   `json:"ok"`
				Redirect string `json:"redirect"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(result.Redirect)
			if err != nil {
				t.Fatal(err)
			}
			if result.OK || u.Path != "/v2/login" || u.Query().Get("next") != "/v2/admin/members/fictional" {
				t.Fatalf("unsafe replay destination: %+v", result)
			}
		})
	}
}

func TestSafeNext(t *testing.T) {
	for _, value := range []string{"https://evil.example", "//evil.example", "/v2/admin/../../login", "/v2/admin/%2e%2e/login", "/v2/admin/\\evil", "/v2/admin\r\nInjected: true", "/other"} {
		if got := SafeNext(value); got != "/v2/admin" {
			t.Errorf("accepted %q => %q", value, got)
		}
	}
	if got := SafeNext("/v2/admin/sales?period=month"); got != "/v2/admin/sales?period=month" {
		t.Fatal(got)
	}
	if strings.Contains(SafeNext("//evil.example"), "evil") {
		t.Fatal("external return path")
	}
}
