package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/sms"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// DeductModal — GET /v2/admin/members/{id}/deduct/modal.
// 활성 프리셋 (금액 · 시술 메모) 을 함께 로드해서 모달에 주입.
// 프리셋 조회 실패는 UI 를 완전히 막지 않도록 log 만 남기고 빈 배열로 진행.
func (h *PageHandlers) DeductModal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	amts, err := h.Deps.Services.Preset.ListActiveDeductAmounts(ctx)
	if err != nil {
		slog.Warn("v2 deduct modal list amounts", "err", err)
	}
	memos, err := h.Deps.Services.Preset.ListActiveServiceMemos(ctx)
	if err != nil {
		slog.Warn("v2 deduct modal list memos", "err", err)
	}

	amtVals := make([]int64, len(amts))
	for i, a := range amts {
		amtVals[i] = a.Amount
	}
	memoLabels := make([]string, len(memos))
	for i, m := range memos {
		memoLabels[i] = m.Label
	}

	m, err := h.Deps.Services.Member.Get(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, http.StatusOK, page.DeductModal(page.DeductModalProps{
		MemberID:     id,
		MemberName:   m.Name,
		BalanceInt:   m.Balance,
		DeductAmts:   amtVals,
		ServiceMemos: memoLabels,
	}))
}

// DeductSubmit — POST /v2/admin/members/{id}/deduct.
// Form:
//   - amount (필수)
//   - service_name (권장, memo 로 저장)
//
// Redirect 303 → /v2/admin/members/{id}.
func (h *PageHandlers) DeductSubmit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		walletFailure(w, r, http.StatusBadRequest, "입력 내용을 확인해 주세요")
		return
	}
	amtStr := strings.ReplaceAll(r.FormValue("amount"), ",", "")
	amount, err := strconv.ParseInt(amtStr, 10, 64)
	if err != nil || amount <= 0 {
		walletFailure(w, r, http.StatusBadRequest, "차감 금액을 입력해 주세요")
		return
	}
	serviceName := strings.TrimSpace(r.FormValue("service_name"))
	if serviceName == "" {
		serviceName = strings.TrimSpace(r.FormValue("memo"))
	}
	createdBy := h.adminID(r)

	result, err := h.Deps.Services.Charge.Deduct(r.Context(), id, amount, serviceName, createdBy)
	if err != nil {
		switch {
		case errors.Is(err, v2service.ErrNotFound):
			walletFailure(w, r, http.StatusNotFound, "회원을 찾을 수 없습니다")
		case errors.Is(err, v2service.ErrInactive):
			walletFailure(w, r, http.StatusConflict, "분실/정지된 회원입니다. 재활성화 후 이용해 주세요")
		case errors.Is(err, v2service.ErrInsufficient):
			walletFailure(w, r, http.StatusUnprocessableEntity, "잔액이 부족합니다. 현재 잔액을 확인해 주세요")
		case errors.Is(err, v2service.ErrInvalidInput):
			walletFailure(w, r, http.StatusBadRequest, "차감 금액을 확인해 주세요")
		default:
			slog.Error("v2 deduct", "err", err, "member", id)
			walletFailure(w, r, http.StatusInternalServerError, "차감하지 못했습니다. 다시 확인해 주세요")
		}
		return
	}
	if h.Deps.SMS != nil && h.Deps.SMS.Enabled() {
		go func() {
			label := serviceName
			if label == "" {
				label = "이용"
			}
			if err := h.Deps.SMS.Send(result.Member.Phone, sms.DeductMessage(result.Member.Name, label, amount, result.NewBalance)); err != nil {
				slog.Error("sms send failed", "member", id)
			}
		}()
	}

	dest := "/v2/admin/members/" + id
	walletSuccess(w, r, dest)
}
