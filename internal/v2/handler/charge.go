package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

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
	name := ""
	if m, gerr := h.Deps.Services.Member.Get(r.Context(), id); gerr == nil {
		name = m.Name
	}
	render(w, r, http.StatusOK, page.ChargeModal(page.ChargeModalProps{
		MemberID:   id,
		MemberName: name,
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
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	planID := strings.TrimSpace(r.FormValue("plan_id"))
	amtStr := strings.ReplaceAll(r.FormValue("amount"), ",", "")

	var (
		planIDPtr *string
		amount    int64
	)
	if planID != "" {
		planIDPtr = &planID
	} else {
		v, err := strconv.ParseInt(amtStr, 10, 64)
		if err != nil || v <= 0 {
			http.Error(w, "plan_id 또는 amount 필수", http.StatusBadRequest)
			return
		}
		amount = v
	}

	// v2는 admin id를 세션에 담지 않음 (env PIN 로그인). createdBy 는 빈값.
	createdBy := ""

	_, err := h.Deps.Services.Charge.Charge(r.Context(), id, planIDPtr, amount, createdBy)
	if err != nil {
		switch {
		case errors.Is(err, v2service.ErrNotFound):
			http.NotFound(w, r)
		case errors.Is(err, v2service.ErrInactive):
			http.Error(w, "회원이 비활성 상태입니다", http.StatusConflict)
		case errors.Is(err, v2service.ErrPlanInactive):
			http.Error(w, "선택한 플랜이 비활성 상태입니다", http.StatusConflict)
		case errors.Is(err, v2service.ErrInvalidInput):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			slog.Error("v2 charge", "err", err, "member", id)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	dest := "/v2/admin/members/" + id
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
	}
}
