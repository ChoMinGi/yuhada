package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"strings"

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
				returnTo := SafeNext(r.URL.RequestURI())
				if r.Method != http.MethodGet {
					// Wallet/card writes cannot be replayed by a GET after login.
					u, _ := url.Parse(returnTo)
					parts := strings.Split(u.Path, "/")
					if len(parts) >= 6 && strings.HasPrefix(u.Path, "/v2/admin/members/") {
						returnTo = strings.Join(parts[:5], "/")
					}
				}
				dest := "/v2/login?" + url.Values{"next": {returnTo}}.Encode()
				if strings.Contains(r.Header.Get("Accept"), "application/json") || r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusUnauthorized)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "로그인이 필요합니다. 다시 로그인해 주세요.", "redirect": dest})
					return
				}
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

// SafeNext keeps all post-login destinations on the v2 admin read surface.
func SafeNext(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || strings.ContainsAny(value, "\\\r\n") ||
		(u.Path != "/v2/admin" && !strings.HasPrefix(u.Path, "/v2/admin/")) {
		return "/v2/admin"
	}
	if path.Clean(u.Path) != u.Path {
		return "/v2/admin"
	}
	return u.RequestURI()
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
