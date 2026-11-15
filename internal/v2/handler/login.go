package handler

import (
	"github.com/alexedwards/scs/v2"
	"net/http"
	"strings"

	legacyauth "github.com/mingicho/yuhada/internal/auth"
	v2auth "github.com/mingicho/yuhada/internal/v2/auth"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// Home — /v2/ (비로그인이면 /v2/login, 로그인이면 /v2/admin).
func (h *PageHandlers) Home(w http.ResponseWriter, r *http.Request) {
	if v2auth.IsAdmin(r.Context(), h.Deps.Session) {
		http.Redirect(w, r, h.defaultAdminPath(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/v2/login", http.StatusSeeOther)
}

// LoginGet — /v2/login.
func (h *PageHandlers) LoginGet(w http.ResponseWriter, r *http.Request) {
	next := h.loginNext(r.URL.Query().Get("next"))
	render(w, r, http.StatusOK, page.Login(page.LoginProps{
		Next:         next,
		KakaoEnabled: h.Deps.Kakao != nil && h.Deps.Kakao.Enabled(),
	}))
}

func (h *PageHandlers) defaultAdminPath() string {
	if h.Deps.AuthOnly {
		return "/admin"
	}
	return "/v2/admin"
}

func (h *PageHandlers) loginNext(value string) string {
	next := legacyauth.SafeAdminNext(value, h.defaultAdminPath())
	if h.Deps.AuthOnly && strings.HasPrefix(next, "/v2/admin") {
		return "/admin"
	}
	return next
}

func (h *PageHandlers) adminID(r *http.Request) string {
	if id := h.Deps.Session.GetString(r.Context(), v2auth.SessionKeyKakaoID); id != "" {
		return "kakao:" + id
	}
	if id := h.Deps.Session.GetString(r.Context(), v2auth.SessionKeyAdminID); id != "" {
		return id
	}
	return "v2-admin"
}

// LogoutPost — /v2/logout.
func (h *PageHandlers) LogoutPost(w http.ResponseWriter, r *http.Request) {
	failed := false
	for _, sm := range []*scs.SessionManager{h.Deps.Session, h.Deps.LegacySession} {
		if sm == nil {
			continue
		}
		if err := sm.Destroy(r.Context()); err != nil {
			failed = true
		}
	}
	if failed {
		http.Error(w, "로그아웃하지 못했습니다. 다시 시도해 주세요.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/v2/login", http.StatusSeeOther)
}
