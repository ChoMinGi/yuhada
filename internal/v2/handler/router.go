// Package handler — v2 HTTP 핸들러.
//
// 격리(isolation-rules.md §1-4): v1 라우터/미들웨어 참조 없음. v2 전용 mux.
// R4: 첫 미들웨어로 chi/middleware.Recoverer 등록 → v2 panic 이 v1 프로세스 죽이지 않음.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/a-h/templ"

	v2auth "github.com/mingicho/yuhada/internal/v2/auth"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	v2sms "github.com/mingicho/yuhada/internal/v2/sms"
)

// Deps — v2 핸들러 공통 의존성.
type Deps struct {
	Session     *scs.SessionManager
	Services    *v2service.Services
	SMS         *v2sms.Client
	AdminPIN    string // cfg.AdminPinV2 (env: ADMIN_PIN_V2)
	EnableDebug bool
}

// PageHandlers — 페이지 + 폼 submit 핸들러 공통 컨테이너.
type PageHandlers struct {
	Deps *Deps
}

// NewRouter — chi 라우터 구성. mount("/v2", ...) 로 marshalled.
func NewRouter(deps *Deps) http.Handler {
	r := chi.NewRouter()

	// R4: v2 panic 이 v1 프로세스로 전파되지 않도록 최우선 미들웨어.
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Compress(5))
	r.Use(deps.Session.LoadAndSave)

	// 헬스체크 (인증 없이 접근).
	r.Get("/healthz", healthz)

	h := &PageHandlers{Deps: deps}

	// ─── 공개 (로그인 페이지) ───
	r.Group(func(r chi.Router) {
		r.Use(v2auth.RedirectIfAuthed(deps.Session, "/v2/admin"))
		r.Get("/", h.Home)
		r.Get("/login", h.LoginGet)
		r.Post("/login", h.LoginPost)
	})
	r.Post("/logout", h.LogoutPost)

	// ─── 관리자 ───
	r.Group(func(r chi.Router) {
		r.Use(v2auth.RequireAdmin(deps.Session))

		r.Get("/admin", h.Dashboard)
		r.Get("/admin/sales", h.SalesDashboard)

		// Members
		r.Route("/admin/members", func(r chi.Router) {
			r.Get("/", h.MembersList)
			r.Get("/new", h.NewMemberGet)
			r.Post("/", h.NewMemberPost)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.MemberDetailGet)
				r.Delete("/", h.MemberDelete)

				r.Get("/memo", h.MemoView)
				r.Get("/memo/edit", h.MemoEdit)
				r.Patch("/memo", h.MemoUpdate)

				// Modals
				r.Get("/charge/modal", h.ChargeModal)
				r.Get("/deduct/modal", h.DeductModal)

				// Wallet ops
				r.Post("/charge", h.ChargeSubmit)
				r.Post("/deduct", h.DeductSubmit)
			})
		})

		// Legacy /admin/plans → /admin/settings/plans (하위호환 302 redirect)
		r.Get("/admin/plans", h.LegacyPlansRedirect)

		// Settings — 설정 하위 메뉴 3개 (충전 보너스 / 차감 금액 / 시술 메모)
		r.Route("/admin/settings", func(r chi.Router) {
			// Plans (충전 보너스 / 가격표 관리)
			r.Route("/plans", func(r chi.Router) {
				r.Get("/", h.PlansList)
				r.Post("/", h.PlanCreate)
				r.Route("/{id}", func(r chi.Router) {
					r.Patch("/", h.PlanUpdate)
					r.Post("/deactivate", h.PlanDeactivate)
					r.Post("/reactivate", h.PlanReactivate)
				})
			})

			// Deduct amount presets (차감 금액 프리셋)
			r.Route("/deduct-amounts", func(r chi.Router) {
				r.Get("/", h.DeductAmountsList)
				r.Post("/", h.DeductAmountCreate)
				r.Post("/reorder", h.DeductAmountReorder) // 드래그 앤 드롭 순서 변경
				r.Route("/{id}", func(r chi.Router) {
					r.Patch("/", h.DeductAmountUpdate)
					r.Delete("/", h.DeductAmountDelete) // 하드 삭제
					r.Post("/deactivate", h.DeductAmountDeactivate)
					r.Post("/reactivate", h.DeductAmountReactivate)
				})
			})

			// Service memo presets (시술 메모 프리셋)
			r.Route("/service-memos", func(r chi.Router) {
				r.Get("/", h.ServiceMemosList)
				r.Post("/", h.ServiceMemoCreate)
				r.Post("/reorder", h.ServiceMemoReorder) // 드래그 앤 드롭 순서 변경
				r.Route("/{id}", func(r chi.Router) {
					r.Patch("/", h.ServiceMemoUpdate)
					r.Delete("/", h.ServiceMemoDelete) // 하드 삭제
					r.Post("/deactivate", h.ServiceMemoDeactivate)
					r.Post("/reactivate", h.ServiceMemoReactivate)
				})
			})
		})
	})

	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("v2 ok"))
}

// ─────────────────────────────────────────────
// v2 자체 render 헬퍼 — v1 internal/httpx 를 import 하지 않기 위한 인라인 유틸.
// (isolation-rules.md §4 화이트리스트에 httpx 없음.)
// ─────────────────────────────────────────────

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		slog.Error("v2 templ render", "err", err, "path", r.URL.Path)
	}
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func hxRedirect(w http.ResponseWriter, url string) {
	w.Header().Set("HX-Redirect", url)
	w.WriteHeader(http.StatusNoContent)
}
