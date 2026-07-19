package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

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

	name := ""
	if m, gerr := h.Deps.Services.Member.Get(ctx, id); gerr == nil {
		name = m.Name
	}
	render(w, r, http.StatusOK, page.DeductModal(page.DeductModalProps{
		MemberID:     id,
		MemberName:   name,
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
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	amtStr := strings.ReplaceAll(r.FormValue("amount"), ",", "")
	amount, err := strconv.ParseInt(amtStr, 10, 64)
	if err != nil || amount <= 0 {
		http.Error(w, "amount 필수", http.StatusBadRequest)
		return
	}
	serviceName := strings.TrimSpace(r.FormValue("service_name"))
	createdBy := ""

	_, err = h.Deps.Services.Charge.Deduct(r.Context(), id, amount, serviceName, createdBy)
	if err != nil {
		switch {
		case errors.Is(err, v2service.ErrNotFound):
			http.NotFound(w, r)
		case errors.Is(err, v2service.ErrInactive):
			http.Error(w, "회원이 비활성 상태입니다", http.StatusConflict)
		case errors.Is(err, v2service.ErrInsufficient):
			http.Error(w, "잔액 부족", http.StatusUnprocessableEntity)
		case errors.Is(err, v2service.ErrInvalidInput):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			slog.Error("v2 deduct", "err", err, "member", id)
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
