package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/db/dbgen"
	"github.com/mingicho/yuhada/internal/httpx"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/view/page"
)

// 잔액 부족 임계값 (이하면 LowBalance 화면).
const lowBalanceThreshold int64 = 30000

// GET /m/{card_uuid} — 고객이 NFC 카드 태깅 후 진입.
//
// 분기 우선순위:
//  1. 카드 자체가 없음           → NotFound
//  2. is_active=0 (분실 신고됨)  → Blocked (매장 방문 안내)
//  3. balance == 0 && tx 없음   → Onboarding (첫 방문)
//  4. balance ≤ 임계값           → LowBalance (경고)
//  5. 기본                       → Tagged (정상 잔액)
func (h *PageHandlers) CustomerCard(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "card_uuid")

	// is_active=1만 매칭하는 service 메서드 → Active 회원 우선 조회.
	m, err := h.Services.Member.GetByCardUUID(r.Context(), uuid)
	if err != nil && !errors.Is(err, service.ErrNotFound) {
		slog.Error("customer card", "err", err, "uuid", uuid)
		http.Error(w, err.Error(), 500)
		return
	}

	// Active 매칭 실패 → 분실 카드일 가능성 체크.
	if errors.Is(err, service.ErrNotFound) {
		if lost, ok := h.findInactiveByCard(r.Context(), uuid); ok {
			renderC(w, r, page.Blocked(page.CustomerView{
				Name:     lost.Name,
				CardUUID: uuid,
			}))
			return
		}
		renderC(w, r, page.CustomerNotFound())
		return
	}

	txs, _ := h.Services.Tx.ListByMember(r.Context(), m.ID)

	view := page.CustomerView{
		Name:       m.Name,
		Balance:    util.FormatKRW(m.Balance),
		BalanceInt: m.Balance,
		CardUUID:   uuid,
		LastCharge: "—",
		LastVisit:  "—",
	}
	for _, t := range txs {
		if view.LastCharge == "—" && t.Type == "charge" {
			view.LastCharge = formatTimeShort(t.CreatedAt) + " · " + util.FormatKRW(t.Amount)
		}
		if view.LastVisit == "—" && t.Type == "deduct" {
			memo := "이용"
			if t.Memo.Valid && t.Memo.String != "" {
				memo = t.Memo.String
			}
			view.LastVisit = formatTimeShort(t.CreatedAt) + " · " + memo
		}
	}

	switch {
	case m.Balance == 0 && len(txs) == 0:
		renderC(w, r, page.Onboarding(view))
	case m.Balance <= lowBalanceThreshold:
		renderC(w, r, page.LowBalance(view))
	default:
		renderC(w, r, page.Tagged(view))
	}
}

// findInactiveByCard — is_active 무관하게 카드로 회원 찾기.
// service에 ListAll만 있어서 in-memory 필터로 처리. 미용실 규모(수백명)면 부담 X.
func (h *PageHandlers) findInactiveByCard(ctx context.Context, uuid string) (dbgen.Member, bool) {
	all, err := h.Services.Member.Search(ctx, "", service.SortDefault)
	if err != nil {
		return dbgen.Member{}, false
	}
	for _, m := range all {
		if m.CardUuid.Valid && m.CardUuid.String == uuid {
			return m, true
		}
	}
	return dbgen.Member{}, false
}

func renderC(w http.ResponseWriter, r *http.Request, c templ.Component) {
	httpx.Render(w, r, http.StatusOK, c)
}
