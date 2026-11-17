package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/sms"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// ChargeModal — GET /v2/admin/members/{id}/charge/modal.
// 활성 플랜 6장 카드 + 기타 금액 입력.
func (h *PageHandlers) ChargeModal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	plans, err := h.Deps.Services.Plan.ListActive(r.Context())
	if err != nil {
		slog.Error("v2 charge modal list plans", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]page.PlanRow, len(plans))
	for i, p := range plans {
		rows[i] = planRowFromDB(p)
	}
	m, err := h.Deps.Services.Member.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, http.StatusOK, page.ChargeModal(page.ChargeModalProps{
		MemberID:   id,
		MemberName: m.Name,
		BalanceInt: m.Balance,
		Plans:      rows,
	}))
}

// ChargeSubmit — POST /v2/admin/members/{id}/charge.
// Form:
//   - plan_id  (있으면 plan 기반 충전 — amount 무시)
//   - amount   (plan_id 없을 때만 사용; 일반 충전)
//
// Redirect 303 → /v2/admin/members/{id}.
func (h *PageHandlers) ChargeSubmit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		walletFailure(w, r, http.StatusBadRequest, "입력 내용을 확인해 주세요")
		return
	}
	planID := strings.TrimSpace(r.FormValue("plan_id"))
	amtStr := strings.ReplaceAll(r.FormValue("amount"), ",", "")

	var (
		planIDPtr *string
		amount    int64
		quote     *v2service.PlanQuote
	)
	if planID != "" {
		planIDPtr = &planID
		price, priceErr := strconv.ParseInt(r.FormValue("expected_price"), 10, 64)
		credit, creditErr := strconv.ParseInt(r.FormValue("expected_credit"), 10, 64)
		version := strings.TrimSpace(r.FormValue("expected_plan_version"))
		if priceErr != nil || creditErr != nil || price <= 0 || credit <= 0 || len(version) != 64 {
			walletFailure(w, r, http.StatusConflict, "상품 정보를 다시 확인해야 합니다. 충전 창을 닫고 다시 열어 주세요.")
			return
		}
		quote = &v2service.PlanQuote{Price: price, CreditAmount: credit, Version: version}
	} else {
		v, err := strconv.ParseInt(amtStr, 10, 64)
		if err != nil || v <= 0 {
			walletFailure(w, r, http.StatusBadRequest, "충전할 상품 또는 금액을 입력해 주세요")
			return
		}
		amount = v
	}

	createdBy := h.adminID(r)
	memo := strings.TrimSpace(r.FormValue("memo"))

	result, err := h.Deps.Services.Charge.ChargeWithExpectedPlan(r.Context(), id, planIDPtr, amount, memo, createdBy, quote)
	if err != nil {
		switch {
		case errors.Is(err, v2service.ErrPlanQuoteChanged):
			walletFailure(w, r, http.StatusConflict, "상품 가격이나 혜택이 변경되었습니다. 충전 창을 닫고 다시 열어 확인해 주세요.")
		case errors.Is(err, v2service.ErrNotFound):
			walletFailure(w, r, http.StatusNotFound, "회원을 찾을 수 없습니다")
		case errors.Is(err, v2service.ErrInactive):
			walletFailure(w, r, http.StatusConflict, "분실/정지된 회원입니다. 재활성화 후 이용해 주세요")
		case errors.Is(err, v2service.ErrPlanInactive):
			walletFailure(w, r, http.StatusConflict, "선택한 상품은 현재 판매 중이 아닙니다")
		case errors.Is(err, v2service.ErrInvalidInput):
			walletFailure(w, r, http.StatusBadRequest, "충전 금액을 확인해 주세요")
		default:
			slog.Error("v2 charge", "err", err, "member", id)
			walletFailure(w, r, http.StatusInternalServerError, "충전하지 못했습니다. 다시 확인해 주세요")
		}
		return
	}
	if h.Deps.SMS != nil && h.Deps.SMS.Enabled() {
		go func() {
			if err := h.Deps.SMS.Send(result.Member.Phone, sms.ChargeMessage(result.Member.Name, result.Transaction.Amount, result.NewBalance)); err != nil {
				slog.Error("sms send failed", "member", id)
			}
		}()
	}

	dest := "/v2/admin/members/" + id
	walletSuccess(w, r, dest)
}

func wantsWalletJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json") || r.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

func walletFailure(w http.ResponseWriter, r *http.Request, status int, message string) {
	if wantsWalletJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": message})
		return
	}
	http.Error(w, message, status)
}

func walletSuccess(w http.ResponseWriter, r *http.Request, dest string) {
	if wantsWalletJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "redirect": dest})
		return
	}
	if isHTMX(r) {
		hxRedirect(w, dest)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// planRowFromDB — dbgen.V2MembershipPlan → page.PlanRow.
func planRowFromDB(p dbgen.V2MembershipPlan) page.PlanRow {
	perk := ""
	if p.PerkLabel.Valid {
		perk = p.PerkLabel.String
	}
	return page.PlanRow{
		ID:            p.ID,
		TierLabel:     p.TierLabel,
		TierAmount:    p.TierAmount,
		PaymentMethod: p.PaymentMethod,
		Price:         p.Price,
		CreditAmount:  p.CreditAmount,
		Bonus:         p.CreditAmount - p.Price,
		PerkLabel:     perk,
		DisplayOrder:  p.DisplayOrder,
		IsActive:      p.IsActive,
		QuoteVersion:  v2service.PlanQuoteFor(p).Version,
	}
}
