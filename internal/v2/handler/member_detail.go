package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// MemberDetailGet — GET /v2/admin/members/{id}.
// 모든 거래를 한 번만 조회하고 상품 스냅샷은 LEFT JOIN으로 표시한다.
func (h *PageHandlers) MemberDetailGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, err := h.Deps.Services.Member.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, v2service.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	charges, err := h.Deps.Services.Membership.ListChargeHistoryWithPlan(r.Context(), id)
	if err != nil {
		slog.Error("v2 list charge history", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	history, err := h.Deps.Services.Queries.ListMemberHistoryWithPlan(r.Context(), id)
	if err != nil {
		http.Error(w, "거래 내역을 불러오지 못했습니다", http.StatusInternalServerError)
		return
	}
	totals, err := h.Deps.Services.Queries.SumMemberTotals(r.Context(), id)
	if err != nil {
		http.Error(w, "거래 합계를 불러오지 못했습니다", http.StatusInternalServerError)
		return
	}
	pm := buildMemberDetail(m, history, totals.TotalIn, totals.TotalOut)
	pm.Memberships = buildMemberChargeHistory(charges)
	render(w, r, http.StatusOK, page.MemberDetail(pm))
}

// Previous charges are shown alongside plan purchases without guessing a plan,
// payment method or bonus from the credited amount.
func buildMemberChargeHistory(charges []dbgen.ListChargeHistoryWithPlanRow) []page.MemberMembership {
	rows := make([]page.MemberMembership, 0, len(charges))
	for _, charge := range charges {
		snap := decodeSnapshot(charge.PlanSnapshot.String)
		label := snap.TierLabel
		if label == "" {
			label = "충전"
			if charge.TxMemo.Valid && strings.TrimSpace(charge.TxMemo.String) != "" {
				label = charge.TxMemo.String
			}
		}
		rows = append(rows, page.MemberMembership{
			ID: charge.TxID, TierLabel: label, PayMethod: snap.PaymentMethod,
			PurchasedAt: formatKST(charge.TxCreatedAt), CreditAmount: util.FormatKRW(charge.TxAmount),
			PerkLabel: snap.PerkLabel, Status: charge.MembershipStatus.String,
		})
	}
	return rows
}

// MemoView — GET /v2/admin/members/{id}/memo.
func (h *PageHandlers) MemoView(w http.ResponseWriter, r *http.Request) {
	pm, ok := h.loadMemberDetail(w, r)
	if !ok {
		return
	}
	render(w, r, http.StatusOK, page.MemberMemoRow(pm))
}

// MemoEdit — GET /v2/admin/members/{id}/memo/edit.
func (h *PageHandlers) MemoEdit(w http.ResponseWriter, r *http.Request) {
	pm, ok := h.loadMemberDetail(w, r)
	if !ok {
		return
	}
	render(w, r, http.StatusOK, page.MemberMemoEdit(pm))
}

// MemoUpdate — PATCH /v2/admin/members/{id}/memo.
func (h *PageHandlers) MemoUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	memo := strings.TrimSpace(r.FormValue("memo"))
	if err := h.Deps.Services.Member.UpdateMemo(r.Context(), id, memo); err != nil {
		slog.Error("v2 update memo", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pm, ok := h.loadMemberDetail(w, r)
	if !ok {
		return
	}
	render(w, r, http.StatusOK, page.MemberMemoRow(pm))
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

// loadMemberDetail — memo partial 재렌더용 최소 조회.
func (h *PageHandlers) loadMemberDetail(w http.ResponseWriter, r *http.Request) (page.MemberDetailProps, bool) {
	id := chi.URLParam(r, "id")
	m, err := h.Deps.Services.Member.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, v2service.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return page.MemberDetailProps{}, false
	}
	return buildMemberDetail(m, nil, 0, 0), true
}

// buildMemberDetail — dbgen 결과를 view prop 으로 매핑.
// 정액권 뱃지는 저장된 구매 시점 스냅샷을 사용한다.
func buildMemberDetail(
	m dbgen.V2Member,
	history []dbgen.ListMemberHistoryWithPlanRow,
	totalIn, totalOut int64,
) page.MemberDetailProps {
	memo := ""
	if m.Memo.Valid {
		memo = m.Memo.String
	}
	card := ""
	if m.CardUuid.Valid {
		card = m.CardUuid.String
	}

	txs := make([]page.MemberTx, 0, len(history))
	for _, row := range history {
		snap := chargeSnapshot{}
		if row.PlanSnapshot.Valid {
			snap = decodeSnapshot(row.PlanSnapshot.String)
		}
		sign := "-"
		if row.TxType == "charge" {
			sign = "+"
		}
		txs = append(txs, page.MemberTx{Type: row.TxType, Amount: sign + util.FormatKRW(row.TxAmount), Memo: nullStrVal(row.TxMemo), At: formatKST(row.TxCreatedAt), TierLabel: snap.TierLabel, PayMethod: snap.PaymentMethod, PerkLabel: snap.PerkLabel})
	}

	return page.MemberDetailProps{
		HistoryURL: "/v2/admin/transactions?" + url.Values{"period": {"all"}, "q": {util.NormalizePhone(m.Phone)}}.Encode(),
		ID:         m.ID,
		JoinedAt:   joinedDate(m.CreatedAt),
		Name:       m.Name,
		Phone:      util.FormatPhone(m.Phone),
		Card:       card,
		Memo:       memo,
		Active:     m.IsActive,
		Balance:    util.FormatKRW(m.Balance),
		BalanceInt: m.Balance,
		TotalIn:    util.FormatKRW(totalIn),
		TotalOut:   util.FormatKRW(totalOut),
		RecentTxs:  txs,
	}
}

type chargeSnapshot struct {
	TierLabel     string `json:"tier_label"`
	TierAmount    int64  `json:"tier_amount"`
	PaymentMethod string `json:"payment_method"`
	Price         int64  `json:"price"`
	CreditAmount  int64  `json:"credit_amount"`
	PerkLabel     string `json:"perk_label"`
}

func decodeSnapshot(s string) chargeSnapshot {
	if s == "" {
		return chargeSnapshot{}
	}
	// perk_label 이 null 일 수 있어 raw 로 받아 처리.
	var raw map[string]any
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return chargeSnapshot{}
	}
	out := chargeSnapshot{}
	if v, ok := raw["tier_label"].(string); ok {
		out.TierLabel = v
	}
	if v, ok := raw["payment_method"].(string); ok {
		out.PaymentMethod = v
	}
	if v, ok := raw["tier_amount"].(float64); ok {
		out.TierAmount = int64(v)
	}
	if v, ok := raw["price"].(float64); ok {
		out.Price = int64(v)
	}
	if v, ok := raw["credit_amount"].(float64); ok {
		out.CreditAmount = int64(v)
	}
	if v, ok := raw["perk_label"].(string); ok {
		out.PerkLabel = v
	}
	return out
}

func nullStrVal(n sql.NullString) string {
	if n.Valid {
		return n.String
	}
	return ""
}

// formatKST — DB ISO8601 UTC (예: "2026-07-12T08:39:47.378Z") 를
// 사장님 읽기용 한글 KST 문자열로 변환. 예: "7월 12일 오후 5:39".
// 파싱 실패 시 원본 반환 (silently degrade).
var kstLoc = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Seoul"); err == nil {
		return l
	}
	return time.FixedZone("KST", 9*3600)
}()

func formatKST(s string) string {
	if s == "" {
		return ""
	}
	// DB 는 strftime('%Y-%m-%dT%H:%M:%fZ', 'now') — 밀리초 3자리 소수점.
	layouts := []string{
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		time.RFC3339Nano,
		time.RFC3339,
	}
	var t time.Time
	var err error
	for _, l := range layouts {
		t, err = time.Parse(l, s)
		if err == nil {
			break
		}
	}
	if err != nil {
		return s
	}
	t = t.In(kstLoc)
	hour := t.Hour()
	ampm := "오전"
	switch {
	case hour == 0:
		hour = 12
	case hour == 12:
		ampm = "오후"
	case hour > 12:
		ampm = "오후"
		hour -= 12
	}
	return fmt.Sprintf("%d월 %d일 %s %d:%02d", int(t.Month()), t.Day(), ampm, hour, t.Minute())
}
