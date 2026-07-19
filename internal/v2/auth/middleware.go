package auth

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
)

// RequireAdmin — v2 관리자 세션이 없으면 /v2/login 으로 리다이렉트.
//
// v1 auth.RequireAdmin 과 시그니처 동일하지만 세션 키/리다이렉트 경로가 v2 전용.
// chi.Mount("/v2", ...) 는 프리픽스를 스트립하므로 라우터 내부에서 등록은 "/login" 이지만
// 브라우저 URL 은 /v2/login 이 되도록 여기서 명시적으로 /v2 prefix 를 붙여준다.
func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := sm.GetString(r.Context(), SessionKeyRole)
			if role != RoleAdmin {
				dest := "/v2/login?next=" + r.URL.RequestURI()
				if r.Header.Get("HX-Request") == "true" {
					w.Header().Set("HX-Redirect", dest)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				http.Redirect(w, r, dest, http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RedirectIfAuthed — 이미 인증된 상태로 /v2/login 방문 시 dest 로.
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
