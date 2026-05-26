package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/mingicho/yuhada/internal/auth"
	"github.com/mingicho/yuhada/internal/httpx"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/sms"
	"github.com/mingicho/yuhada/internal/view/page"
)

// PageHandlers — 전체 페이지 + 폼 submit 핸들러.
type PageHandlers struct {
	Services *service.Services
	Session  *scs.SessionManager
	SMS      *sms.Client
}

// GET / — 비로그인이면 로그인, 로그인된 상태면 admin 으로.
func (h *PageHandlers) Home(w http.ResponseWriter, r *http.Request) {
	if h.Session.GetString(r.Context(), auth.SessionKeyRole) == auth.RoleAdmin {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// GET /login — PIN 입력 화면.
func (h *PageHandlers) LoginGet(w http.ResponseWriter, r *http.Request) {
	httpx.Render(w, r, http.StatusOK, page.Login(page.LoginProps{}))
}

// POST /login — PIN 검증.
func (h *PageHandlers) LoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	pin := r.FormValue("pin")

	admin, err := h.Services.Admin.VerifyPIN(r.Context(), pin)
	if err != nil {
		msg := "PIN이 올바르지 않습니다"
		if !errors.Is(err, service.ErrPasswordInvalid) {
			slog.Error("login verify", "err", err)
			msg = "로그인 처리 중 오류가 발생했습니다"
		}
		httpx.Render(w, r, http.StatusUnauthorized, page.Login(page.LoginProps{
			ErrorMsg: msg,
		}))
		return
	}

	// 세션 재생성 + 역할 주입
	if err := h.Session.RenewToken(r.Context()); err != nil {
		slog.Error("session renew", "err", err)
	}
	h.Session.Put(r.Context(), auth.SessionKeyRole, auth.RoleAdmin)
	h.Session.Put(r.Context(), auth.SessionKeyAdminID, admin.ID)
	h.Session.Put(r.Context(), auth.SessionKeyEmail, admin.Email)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /logout
func (h *PageHandlers) LogoutPost(w http.ResponseWriter, r *http.Request) {
	if err := h.Session.Destroy(r.Context()); err != nil {
		slog.Error("session destroy", "err", err)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
