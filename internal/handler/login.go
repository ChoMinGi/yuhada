package handler

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/alexedwards/scs/v2"

	"github.com/mingicho/yuhada/internal/auth"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/sms"
)

// PageHandlers — 전체 페이지 + 폼 submit 핸들러.
type PageHandlers struct {
	Services     *service.Services
	Session      *scs.SessionManager
	SMS          *sms.Client
	KakaoSession *scs.SessionManager
}

// GET / — 비로그인이면 로그인, 로그인된 상태면 admin 으로.
func (h *PageHandlers) Home(w http.ResponseWriter, r *http.Request) {
	if auth.IsAdmin(r.Context(), h.Session) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// GET /login — 신규 카카오 로그인 진입점으로 이동.
func (h *PageHandlers) LoginGet(w http.ResponseWriter, r *http.Request) {
	destination := "/v2/login"
	if next := r.URL.Query().Get("next"); next != "" {
		destination += "?" + url.Values{"next": {auth.SafeAdminNext(next, "/v2/admin")}}.Encode()
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

// POST /logout
func (h *PageHandlers) LogoutPost(w http.ResponseWriter, r *http.Request) {
	failed := false
	for _, sm := range []*scs.SessionManager{h.Session, h.KakaoSession} {
		if sm == nil {
			continue
		}
		if err := sm.Destroy(r.Context()); err != nil {
			slog.Error("session destroy", "err", err)
			failed = true
		}
	}
	if failed {
		http.Error(w, "로그아웃하지 못했습니다. 다시 시도해 주세요.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
