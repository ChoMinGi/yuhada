package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/alexedwards/scs/v2"
)

// 세션 키.
const (
	SessionKeyRole       = "role"
	SessionKeyAdminID    = "admin_id"
	SessionKeyEmail      = "email"
	SessionKeyAuthMethod = "auth_method"
	AuthMethodKakao      = "kakao"

	RoleAdmin    = "admin"
	RoleCustomer = "customer"
)

// IsAdmin excludes sessions created by the retired PIN login.
func IsAdmin(ctx context.Context, sm *scs.SessionManager) bool {
	return sm.GetString(ctx, SessionKeyRole) == RoleAdmin && sm.GetString(ctx, SessionKeyAuthMethod) == AuthMethodKakao
}

// SafeAdminNext allows only local, canonical administrator paths.
func SafeAdminNext(value, fallback string) string {
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || strings.ContainsAny(value, "\\\r\n") || path.Clean(u.Path) != u.Path {
		return fallback
	}
	if u.Path != "/admin" && !strings.HasPrefix(u.Path, "/admin/") && u.Path != "/v2/admin" && !strings.HasPrefix(u.Path, "/v2/admin/") {
		return fallback
	}
	return u.RequestURI()
}

// AdminReturnTo never asks the browser to replay a write after sign-in.
func AdminReturnTo(r *http.Request, fallback string) string {
	value := SafeAdminNext(r.URL.RequestURI(), fallback)
	if r.Method == http.MethodGet {
		return value
	}
	u, _ := url.Parse(value)
	for _, prefix := range []string{"/admin/members/", "/v2/admin/members/"} {
		if strings.HasPrefix(u.Path, prefix) {
			return prefix + strings.Split(strings.TrimPrefix(u.Path, prefix), "/")[0]
		}
	}
	if strings.HasPrefix(u.Path, "/v2/admin/settings/") {
		return "/v2/admin/settings/" + strings.Split(strings.TrimPrefix(u.Path, "/v2/admin/settings/"), "/")[0]
	}
	return SafeAdminNext(u.Path, fallback)
}

// RequireAdmin — 관리자 세션 없으면 /login으로 리다이렉트 (HTMX 고려).
func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !IsAdmin(r.Context(), sm) {
				redirectURL := "/login?" + url.Values{"next": {AdminReturnTo(r, "/admin")}}.Encode()
				if strings.Contains(r.Header.Get("Accept"), "application/json") || r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusUnauthorized)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "카카오 로그인이 필요합니다.", "redirect": redirectURL})
					return
				}
				if r.Header.Get("HX-Request") == "true" {
					w.Header().Set("HX-Redirect", redirectURL)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				http.Redirect(w, r, redirectURL, http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RedirectIfAuthed — 로그인된 상태로 /login 방문 시 /admin으로.
func RedirectIfAuthed(sm *scs.SessionManager, dest string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsAdmin(r.Context(), sm) {
				http.Redirect(w, r, dest, http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AdminIDFromContext — 세션에서 현재 로그인한 admin id 꺼내기.
func AdminIDFromContext(ctx context.Context, sm *scs.SessionManager) string {
	return sm.GetString(ctx, SessionKeyAdminID)
}
