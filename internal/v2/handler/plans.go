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

// PlansList — GET /v2/admin/plans (비활성 포함).
func (h *PageHandlers) PlansList(w http.ResponseWriter, r *http.Request) {
	plans, err := h.Deps.Services.Plan.ListAll(r.Context())
	if err != nil {
		slog.Error("v2 plans list", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]page.PlanRow, len(plans))
	for i, p := range plans {
		rows[i] = planRowFromDB(p)
	}
	render(w, r, http.StatusOK, page.Plans(page.PlansProps{Rows: rows}))
}

// PlanCreate — POST /v2/admin/plans.
// Form: tier_label, tier_amount, payment_method, price, credit_amount, perk_label, display_order.
func (h *PageHandlers) PlanCreate(w http.ResponseWriter, r *http.Request) {
	in, err := parsePlanForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := h.Deps.Services.Plan.Create(r.Context(), in); err != nil {
		if errors.Is(err, v2service.ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Error("v2 plan create", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dest := "/v2/admin/plans"
	if isHTMX(r) {
		hxRedirect(w, dest)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// PlanUpdate — PATCH /v2/admin/plans/{id}.
func (h *PageHandlers) PlanUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	in, err := parsePlanForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Deps.Services.Plan.Update(r.Context(), id, in); err != nil {
		if errors.Is(err, v2service.ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Error("v2 plan update", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dest := "/v2/admin/plans"
	if isHTMX(r) {
		hxRedirect(w, dest)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// PlanDeactivate — POST /v2/admin/plans/{id}/deactivate.
func (h *PageHandlers) PlanDeactivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Deps.Services.Plan.Deactivate(r.Context(), id); err != nil {
		slog.Error("v2 plan deactivate", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dest := "/v2/admin/plans"
	if isHTMX(r) {
		hxRedirect(w, dest)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// PlanReactivate — POST /v2/admin/plans/{id}/reactivate.
func (h *PageHandlers) PlanReactivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Deps.Services.Plan.Reactivate(r.Context(), id); err != nil {
		slog.Error("v2 plan reactivate", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dest := "/v2/admin/plans"
	if isHTMX(r) {
		hxRedirect(w, dest)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// parsePlanForm — plan CRUD 폼 파싱 (create/update 공용).
func parsePlanForm(r *http.Request) (v2service.CreatePlanInput, error) {
	if err := r.ParseForm(); err != nil {
		return v2service.CreatePlanInput{}, errors.New("bad form")
	}
	tierAmount, _ := strconv.ParseInt(strings.ReplaceAll(r.FormValue("tier_amount"), ",", ""), 10, 64)
	price, _ := strconv.ParseInt(strings.ReplaceAll(r.FormValue("price"), ",", ""), 10, 64)
	credit, _ := strconv.ParseInt(strings.ReplaceAll(r.FormValue("credit_amount"), ",", ""), 10, 64)
	order, _ := strconv.ParseInt(r.FormValue("display_order"), 10, 64)
	return v2service.CreatePlanInput{
		TierLabel:     strings.TrimSpace(r.FormValue("tier_label")),
		TierAmount:    tierAmount,
		PaymentMethod: strings.TrimSpace(r.FormValue("payment_method")),
		Price:         price,
		CreditAmount:  credit,
		PerkLabel:     strings.TrimSpace(r.FormValue("perk_label")),
		DisplayOrder:  order,
	}, nil
}
