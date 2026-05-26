package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/auth"
	"github.com/mingicho/yuhada/internal/httpx"
	"github.com/mingicho/yuhada/internal/sms"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/view/page"
	"github.com/mingicho/yuhada/internal/view/partial"
)

// ─────────────────────────────────────────────
// Card issuance / lost / reactivate
// ─────────────────────────────────────────────
func (h *PageHandlers) CardIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	uuid := util.NewCardUUID()
	if err := h.Services.Member.IssueCard(r.Context(), id, uuid); err != nil {
		slog.Error("issue card", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, page.MemberCardRow(m))
}

func (h *PageHandlers) CardLost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Services.Member.ReportLost(r.Context(), id); err != nil {
		slog.Error("report lost", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}
	// 전체 페이지 재로드 (헤더 액션 영역도 바뀌므로 hx-target=body)
	w.Header().Set("HX-Redirect", "/admin/members/"+id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *PageHandlers) CardReactivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Services.Member.Reactivate(r.Context(), id); err != nil {
		slog.Error("reactivate", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("HX-Redirect", "/admin/members/"+id)
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────────────────────────
// Modals (GET /admin/members/{id}/{kind}/modal)
// ─────────────────────────────────────────────
func (h *PageHandlers) ChargeModal(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, partial.ChargeModal(m))
}

func (h *PageHandlers) DeductModal(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, partial.DeductModal(m))
}

func (h *PageHandlers) LostModal(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, partial.LostModal(m))
}

func (h *PageHandlers) ReactivateModal(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, partial.ReactivateModal(m))
}

// ─────────────────────────────────────────────
// Charge / Deduct submit
// ─────────────────────────────────────────────
func (h *PageHandlers) ChargeSubmit(w http.ResponseWriter, r *http.Request) {
	h.walletSubmit(w, r, true)
}

func (h *PageHandlers) DeductSubmit(w http.ResponseWriter, r *http.Request) {
	h.walletSubmit(w, r, false)
}

func (h *PageHandlers) walletSubmit(w http.ResponseWriter, r *http.Request, isCharge bool) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	// 입력 값에 천단위 콤마가 들어올 수 있어 strip 후 파싱.
	amtStr := strings.ReplaceAll(r.FormValue("amount"), ",", "")
	amt, err := strconv.ParseInt(amtStr, 10, 64)
	if err != nil || amt <= 0 {
		http.Error(w, "amount required", 400)
		return
	}
	memo := strings.TrimSpace(r.FormValue("memo"))
	adminID := auth.AdminIDFromContext(r.Context(), h.Session)

	var werr error
	if isCharge {
		_, werr = h.Services.Wallet.Charge(r.Context(), id, amt, memo, adminID)
	} else {
		_, werr = h.Services.Wallet.Deduct(r.Context(), id, amt, memo, adminID)
	}
	if werr != nil {
		slog.Error("wallet op", "err", werr, "id", id, "charge", isCharge)
		http.Error(w, werr.Error(), 400)
		return
	}

	// 성공: 잔액 카드 + 거래 내역 한 번에 갱신 + 모달 닫기 트리거.
	// 잔액 카드는 메인 swap, 거래 내역은 hx-swap-oob 로 자동 갱신.
	dbm, err := h.Services.Member.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	txs, _ := h.Services.Tx.ListByMember(r.Context(), id)
	m := buildMemberView(dbm, txs)

	// SMS 알림 (비동기)
	if h.SMS.Enabled() {
		go func() {
			var msg string
			if isCharge {
				msg = sms.ChargeMessage(m.Name, amt, m.BalanceInt)
			} else {
				svcName := memo
				if svcName == "" {
					svcName = "이용"
				}
				msg = sms.DeductMessage(m.Name, svcName, amt, m.BalanceInt)
			}
			if err := h.SMS.Send(dbm.Phone, msg); err != nil {
				slog.Error("sms send failed", "err", err, "member", id)
			}
		}()
	}

	w.Header().Set("HX-Trigger", "closemodal")
	httpx.Render(w, r, http.StatusOK, page.WalletResult(m))
}
