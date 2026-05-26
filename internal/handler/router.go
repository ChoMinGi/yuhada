package handler

import (
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"

	"github.com/mingicho/yuhada/internal/auth"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/sms"
)

// Deps — 핸들러 공통 의존성.
type Deps struct {
	Session     *scs.SessionManager
	Services    *service.Services
	SMS         *sms.Client
	EnableDebug bool // dev에서만 /debug/* 노출
}

// NewRouter — chi 라우터 구성.
func NewRouter(deps *Deps) http.Handler {
	r := chi.NewRouter()

	// 공통 미들웨어
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Compress(5))
	r.Use(deps.Session.LoadAndSave)

	// 정적 파일 (CSS, fonts, icons 등)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	// 헬스체크
	r.Get("/healthz", healthz)

	pages := &PageHandlers{
		Services: deps.Services,
		Session:  deps.Session,
		SMS:      deps.SMS,
	}

	// ─── 공개 ───
	r.Get("/", pages.Home)

	// 로그인 — 이미 인증된 상태면 /admin으로
	r.Group(func(r chi.Router) {
		r.Use(auth.RedirectIfAuthed(deps.Session, "/admin"))
		r.Get("/login", pages.LoginGet)
		// POST /login 은 IP 기준 분당 10회 rate limit
		r.With(httprate.LimitByIP(10, 1*time.Minute)).Post("/login", pages.LoginPost)
	})
	r.Post("/logout", pages.LogoutPost)

	// ─── 고객 모바일 (NFC 태깅 진입) ───
	// 자체 로그인 없음 — 카드 UUID만으로 잔액 조회.
	// 분실 카드는 Blocked 화면으로 매장 방문 안내.
	r.Get("/m/{card_uuid}", pages.CustomerCard)

	// ─── 관리자 ───
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin(deps.Session))

		r.Get("/admin", pages.AdminHome)
		r.Get("/admin/cards", pages.CardsList)
		r.Get("/admin/transactions", pages.TransactionsPage)

		// Members
		r.Route("/admin/members", func(r chi.Router) {
			r.Get("/", pages.MembersList)
			r.Get("/new", pages.NewMemberGet)
			r.Post("/", pages.NewMemberPost)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", pages.MemberDetailGet)
				r.Delete("/", pages.MemberDelete)

				// Memo
				r.Get("/memo", pages.MemoView)
				r.Get("/memo/edit", pages.MemoEdit)
				r.Patch("/memo", pages.MemoUpdate)

				// Card
				r.Post("/card", pages.CardIssue)
				r.Post("/lost", pages.CardLost)
				r.Post("/reactivate", pages.CardReactivate)

				// Modals
				r.Get("/charge/modal", pages.ChargeModal)
				r.Get("/deduct/modal", pages.DeductModal)
				r.Get("/lost/modal", pages.LostModal)
				r.Get("/reactivate/modal", pages.ReactivateModal)
				r.Get("/delete/modal", pages.DeleteModal)

				// Wallet ops
				r.Post("/charge", pages.ChargeSubmit)
				r.Post("/deduct", pages.DeductSubmit)
			})
		})
	})

	// ─── 개발·검증용 ───
	if deps.EnableDebug {
		debug := &DebugHandlers{Services: deps.Services}
		r.Route("/debug", func(r chi.Router) {
			r.Get("/members", debug.ListMembers)
			r.Get("/dashboard", debug.Dashboard)
			r.Get("/recent", debug.RecentTransactions)
			r.Post("/charge", debug.Charge)
			r.Post("/deduct", debug.Deduct)
		})
	}

	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
