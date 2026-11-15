package handler

import (
	"crypto/subtle"
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
	next := v2auth.SafeNext(r.URL.Query().Get("next"))
	render(w, r, http.StatusOK, page.Login(page.LoginProps{
		Next:         next,
		KakaoEnabled: h.Deps.Kakao != nil && h.Deps.Kakao.Enabled(),
	}))
}

// LoginPost — PIN 검증 (env ADMIN_PIN_V2 와 단순 비교).
// v2 는 admin_users 테이블 사용 안 함 (단순화, isolation-rules §1-5).
func (h *PageHandlers) LoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	pin := strings.TrimSpace(r.FormValue("pin"))
	next := v2auth.SafeNext(strings.TrimSpace(r.FormValue("next")))
	adminID := ""
	valid := false
	if h.Deps.Legacy != nil {
		admin, err := h.Deps.Legacy.Admin.VerifyPIN(r.Context(), pin)
		valid = err == nil
		adminID = admin.ID
	} else if len(pin) == 6 && len(h.Deps.AdminPIN) == 6 {
		// Standalone fixtures; production always verifies the existing admin hash.
		valid = subtle.ConstantTimeCompare([]byte(pin), []byte(h.Deps.AdminPIN)) == 1
	}

	if !valid {
		render(w, r, http.StatusUnauthorized, page.Login(page.LoginProps{
			ErrorMsg:     "PIN이 올바르지 않습니다",
			Next:         next,
			KakaoEnabled: h.Deps.Kakao != nil && h.Deps.Kakao.Enabled(),
		}))
		return
	}

	// 세션 회전 (session fixation 방어).
	if err := h.Deps.Session.RenewToken(r.Context()); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	h.Deps.Session.Put(r.Context(), v2auth.SessionKeyRole, v2auth.RoleAdmin)
	if adminID != "" {
		h.Deps.Session.Put(r.Context(), v2auth.SessionKeyAdminID, adminID)
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (h *PageHandlers) adminID(r *http.Request) string {
	if id := h.Deps.Session.GetString(r.Context(), v2auth.SessionKeyAdminID); id != "" {
		return id
	}
	if id := h.Deps.Session.GetString(r.Context(), "v2_kakao_id"); id != "" {
		return "kakao:" + id
	}
	return "v2-admin"
}

// LogoutPost — /v2/logout.
func (h *PageHandlers) LogoutPost(w http.ResponseWriter, r *http.Request) {
	_ = h.Deps.Session.Destroy(r.Context())
	http.Redirect(w, r, "/v2/login", http.StatusSeeOther)
}
