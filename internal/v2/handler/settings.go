package handler

import (
	"encoding/json"
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

// ═════════════════════════════════════════════════════════════
// Deduct amount presets — /v2/admin/settings/deduct-amounts
// ═════════════════════════════════════════════════════════════

// DeductAmountsList — GET /v2/admin/settings/deduct-amounts.
func (h *PageHandlers) DeductAmountsList(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Deps.Services.Preset.ListAllDeductAmounts(r.Context())
	if err != nil {
		slog.Error("v2 deduct amounts list", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]page.DeductAmountRow, len(rows))
	for i, r := range rows {
		items[i] = deductAmountRowFromDB(r)
	}
	render(w, r, http.StatusOK, page.SettingsDeductAmounts(page.SettingsDeductAmountsProps{Rows: items}))
}

// DeductAmountCreate — POST /v2/admin/settings/deduct-amounts.
// Form: amount, display_order.
func (h *PageHandlers) DeductAmountCreate(w http.ResponseWriter, r *http.Request) {
	amount, order, err := parseDeductAmountForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Deps.Services.Preset.CreateDeductAmount(r.Context(), amount, order); err != nil {
		if errors.Is(err, v2service.ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Error("v2 deduct amount create", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/deduct-amounts")
}

// DeductAmountUpdate — PATCH /v2/admin/settings/deduct-amounts/{id}.
func (h *PageHandlers) DeductAmountUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	amount, order, err := parseDeductAmountForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Deps.Services.Preset.UpdateDeductAmount(r.Context(), id, amount, order); err != nil {
		if errors.Is(err, v2service.ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Error("v2 deduct amount update", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/deduct-amounts")
}

// DeductAmountDeactivate — POST /v2/admin/settings/deduct-amounts/{id}/deactivate.
func (h *PageHandlers) DeductAmountDeactivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Deps.Services.Preset.DeactivateDeductAmount(r.Context(), id); err != nil {
		slog.Error("v2 deduct amount deactivate", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/deduct-amounts")
}

// DeductAmountReactivate — POST /v2/admin/settings/deduct-amounts/{id}/reactivate.
func (h *PageHandlers) DeductAmountReactivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Deps.Services.Preset.ReactivateDeductAmount(r.Context(), id); err != nil {
		slog.Error("v2 deduct amount reactivate", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/deduct-amounts")
}

// DeductAmountDelete — DELETE /v2/admin/settings/deduct-amounts/{id}. Hard delete.
func (h *PageHandlers) DeductAmountDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Deps.Services.Preset.DeleteDeductAmount(r.Context(), id); err != nil {
		slog.Error("v2 deduct amount delete", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/deduct-amounts")
}

// DeductAmountReorder — POST /v2/admin/settings/deduct-amounts/reorder.
// Body: JSON {"order": ["id1","id2",...]}. SortableJS onEnd 훅에서 호출.
func (h *PageHandlers) DeductAmountReorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order []string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := h.Deps.Services.Preset.ReorderDeductAmounts(r.Context(), body.Order); err != nil {
		slog.Error("v2 deduct amount reorder", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ═════════════════════════════════════════════════════════════
// Service memo presets — /v2/admin/settings/service-memos
// ═════════════════════════════════════════════════════════════

// ServiceMemosList — GET /v2/admin/settings/service-memos.
func (h *PageHandlers) ServiceMemosList(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Deps.Services.Preset.ListAllServiceMemos(r.Context())
	if err != nil {
		slog.Error("v2 service memos list", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]page.ServiceMemoRow, len(rows))
	for i, r := range rows {
		items[i] = serviceMemoRowFromDB(r)
	}
	render(w, r, http.StatusOK, page.SettingsServiceMemos(page.SettingsServiceMemosProps{Rows: items}))
}

// ServiceMemoCreate — POST /v2/admin/settings/service-memos.
// Form: label, display_order.
func (h *PageHandlers) ServiceMemoCreate(w http.ResponseWriter, r *http.Request) {
	label, order, err := parseServiceMemoForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Deps.Services.Preset.CreateServiceMemo(r.Context(), label, order); err != nil {
		if errors.Is(err, v2service.ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Error("v2 service memo create", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/service-memos")
}

// ServiceMemoUpdate — PATCH /v2/admin/settings/service-memos/{id}.
func (h *PageHandlers) ServiceMemoUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	label, order, err := parseServiceMemoForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Deps.Services.Preset.UpdateServiceMemo(r.Context(), id, label, order); err != nil {
		if errors.Is(err, v2service.ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Error("v2 service memo update", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/service-memos")
}

// ServiceMemoDeactivate — POST /v2/admin/settings/service-memos/{id}/deactivate.
func (h *PageHandlers) ServiceMemoDeactivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Deps.Services.Preset.DeactivateServiceMemo(r.Context(), id); err != nil {
		slog.Error("v2 service memo deactivate", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/service-memos")
}

// ServiceMemoReactivate — POST /v2/admin/settings/service-memos/{id}/reactivate.
func (h *PageHandlers) ServiceMemoReactivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Deps.Services.Preset.ReactivateServiceMemo(r.Context(), id); err != nil {
		slog.Error("v2 service memo reactivate", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/service-memos")
}

// ServiceMemoDelete — DELETE /v2/admin/settings/service-memos/{id}. Hard delete.
func (h *PageHandlers) ServiceMemoDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Deps.Services.Preset.DeleteServiceMemo(r.Context(), id); err != nil {
		slog.Error("v2 service memo delete", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectSettings(w, r, "/v2/admin/settings/service-memos")
}

// ServiceMemoReorder — POST /v2/admin/settings/service-memos/reorder.
// Body: JSON {"order": ["id1","id2",...]}. SortableJS onEnd 훅에서 호출.
func (h *PageHandlers) ServiceMemoReorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order []string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := h.Deps.Services.Preset.ReorderServiceMemos(r.Context(), body.Order); err != nil {
		slog.Error("v2 service memo reorder", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ═════════════════════════════════════════════════════════════
// Redirect from legacy /admin/plans → /admin/settings/plans
// ═════════════════════════════════════════════════════════════

// LegacyPlansRedirect — 이전 /v2/admin/plans 링크 호환용 302 리다이렉트.
func (h *PageHandlers) LegacyPlansRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/v2/admin/settings/plans", http.StatusFound)
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func parseDeductAmountForm(r *http.Request) (amount int64, order int64, err error) {
	if err := r.ParseForm(); err != nil {
		return 0, 0, errors.New("bad form")
	}
	amt, e := strconv.ParseInt(strings.ReplaceAll(r.FormValue("amount"), ",", ""), 10, 64)
	if e != nil {
		return 0, 0, errors.New("amount required")
	}
	ord, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("display_order")), 10, 64)
	return amt, ord, nil
}

func parseServiceMemoForm(r *http.Request) (label string, order int64, err error) {
	if err := r.ParseForm(); err != nil {
		return "", 0, errors.New("bad form")
	}
	label = strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		return "", 0, errors.New("label required")
	}
	ord, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("display_order")), 10, 64)
	return label, ord, nil
}

func redirectSettings(w http.ResponseWriter, r *http.Request, dest string) {
	if isHTMX(r) {
		hxRedirect(w, dest)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// deductAmountRowFromDB — dbgen.DeductAmountPreset → page.DeductAmountRow.
func deductAmountRowFromDB(p dbgen.DeductAmountPreset) page.DeductAmountRow {
	return page.DeductAmountRow{
		ID:           p.ID,
		Amount:       p.Amount,
		DisplayOrder: p.DisplayOrder,
		IsActive:     p.IsActive,
	}
}

// serviceMemoRowFromDB — dbgen.ServiceMemoPreset → page.ServiceMemoRow.
func serviceMemoRowFromDB(p dbgen.ServiceMemoPreset) page.ServiceMemoRow {
	return page.ServiceMemoRow{
		ID:           p.ID,
		Label:        p.Label,
		DisplayOrder: p.DisplayOrder,
		IsActive:     p.IsActive,
	}
}
