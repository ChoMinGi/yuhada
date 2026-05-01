package auth

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"
)

// 세션 키.
const (
	SessionKeyRole    = "role"
	SessionKeyAdminID = "admin_id"
	SessionKeyEmail   = "email"

	RoleAdmin    = "admin"
	RoleCustomer = "customer"
)

// RequireAdmin — 관리자 세션 없으면 /login으로 리다이렉트 (HTMX 고려).
func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := sm.GetString(r.Context(), SessionKeyRole)
			if role != RoleAdmin {
				next := r.URL.RequestURI()
				redirectURL := "/login?next=" + next
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
			if sm.GetString(r.Context(), SessionKeyRole) == RoleAdmin {
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
