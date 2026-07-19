package handler

import (
	"net/http"
	"strings"

	v2auth "github.com/mingicho/yuhada/internal/v2/auth"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// Home — /v2/ (비로그인이면 /v2/login, 로그인이면 /v2/admin).
func (h *PageHandlers) Home(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Session.GetString(r.Context(), v2auth.SessionKeyRole) == v2auth.RoleAdmin {
		http.Redirect(w, r, "/v2/admin", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/v2/login", http.StatusSeeOther)
}

// LoginGet — /v2/login.
func (h *PageHandlers) LoginGet(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	render(w, r, http.StatusOK, page.Login(page.LoginProps{Next: next}))
}

// LoginPost — PIN 검증 (env ADMIN_PIN_V2 와 단순 비교).
// v2 는 admin_users 테이블 사용 안 함 (단순화, isolation-rules §1-5).
func (h *PageHandlers) LoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	pin := strings.TrimSpace(r.FormValue("pin"))
	next := strings.TrimSpace(r.FormValue("next"))
	if next == "" {
		next = "/v2/admin"
	}

	if pin == "" || pin != h.Deps.AdminPIN {
		render(w, r, http.StatusUnauthorized, page.Login(page.LoginProps{
			ErrorMsg: "PIN이 올바르지 않습니다",
			Next:     next,
		}))
		return
	}

	// 세션 회전 (session fixation 방어).
	if err := h.Deps.Session.RenewToken(r.Context()); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	h.Deps.Session.Put(r.Context(), v2auth.SessionKeyRole, v2auth.RoleAdmin)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// LogoutPost — /v2/logout.
func (h *PageHandlers) LogoutPost(w http.ResponseWriter, r *http.Request) {
	_ = h.Deps.Session.Destroy(r.Context())
	http.Redirect(w, r, "/v2/login", http.StatusSeeOther)
}
