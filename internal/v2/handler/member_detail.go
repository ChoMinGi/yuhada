package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// MemberDetailGet — GET /v2/admin/members/{id}.
// R3 준수: 최근 거래는 ListChargeHistoryWithPlan 로 LEFT JOIN 소비.
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

	memberships, err := h.Deps.Services.Membership.ListByMember(r.Context(), id)
	if err != nil {
		slog.Error("v2 list memberships", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	chargeRows, err := h.Deps.Services.Membership.ListChargeHistoryWithPlan(r.Context(), id)
	if err != nil {
		slog.Error("v2 list charge history", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 차감 이력도 표시하기 위해 transactions 직접 조회 (LEFT JOIN 은 charge 만).
	deducts, err := h.Deps.Services.Queries.ListTransactionsByMember(r.Context(), id)
	if err != nil {
		slog.Error("v2 list txs", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pm := buildMemberDetail(m, memberships, chargeRows, deducts)
	render(w, r, http.StatusOK, page.MemberDetail(pm))
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
	return buildMemberDetail(m, nil, nil, nil), true
}

// buildMemberDetail — dbgen 결과를 view prop 으로 매핑.
// 정액권 뱃지: chargeRows 의 PlanSnapshot 이 유효하면 json.Unmarshal 로 파싱.
func buildMemberDetail(
	m dbgen.V2Member,
	memberships []dbgen.V2Membership,
	chargeRows []dbgen.ListChargeHistoryWithPlanRow,
	deducts []dbgen.V2Transaction,
) page.MemberDetailProps {
	memo := ""
	if m.Memo.Valid {
		memo = m.Memo.String
	}
	card := ""
	if m.CardUuid.Valid {
		card = m.CardUuid.String
	}

	// MEMBERSHIPS 섹션.
	mships := make([]page.MemberMembership, 0, len(memberships))
	for _, ms := range memberships {
		snap := decodeSnapshot(ms.PlanSnapshot)
		mships = append(mships, page.MemberMembership{
			ID:           ms.ID,
			TierLabel:    snap.TierLabel,
			PayMethod:    snap.PaymentMethod,
			PurchasedAt:  formatKST(ms.PurchasedAt),
			CreditAmount: util.FormatKRW(snap.CreditAmount),
			PerkLabel:    snap.PerkLabel,
			Status:       ms.Status,
		})
	}

	// 최근 거래 (charge LEFT JOIN memberships + deduct/refund).
	//
	// v2 상세 페이지는 charge 는 R3 LEFT JOIN 결과로,
	// deduct/refund 는 transactions 직접 조회로 합쳐서 표시한다.
	// 두 소스를 병합한 뒤 원본 created_at (ISO8601 UTC 사전순 = 시간순) 으로
	// 최신순 정렬해서 사장님이 시간 흐름대로 보게 한다.
	type txWithRaw struct {
		tx  page.MemberTx
		raw string
	}
	var (
		totalIn, totalOut int64
		buf               []txWithRaw
	)
	for _, row := range chargeRows {
		snap := chargeSnapshot{}
		if row.PlanSnapshot.Valid {
			snap = decodeSnapshot(row.PlanSnapshot.String)
		}
		buf = append(buf, txWithRaw{
			tx: page.MemberTx{
				Type:      row.TxType,
				Amount:    "+" + util.FormatKRW(row.TxAmount),
				Memo:      nullStrVal(row.TxMemo),
				At:        formatKST(row.TxCreatedAt),
				TierLabel: snap.TierLabel,
				PayMethod: snap.PaymentMethod,
				PerkLabel: snap.PerkLabel,
			},
			raw: row.TxCreatedAt,
		})
		totalIn += row.TxAmount
	}
	for _, t := range deducts {
		if t.Type == "charge" {
			// charge 는 chargeRows 로 이미 포함됨.
			continue
		}
		sign := "-"
		if t.Type == "refund" {
			// refund 는 잔액 차감이지만 label 은 별도 태그.
			sign = "-"
			totalOut += t.Amount
		} else {
			totalOut += t.Amount
		}
		buf = append(buf, txWithRaw{
			tx: page.MemberTx{
				Type:   t.Type,
				Amount: sign + util.FormatKRW(t.Amount),
				Memo:   nullStrVal(t.Memo),
				At:     formatKST(t.CreatedAt),
			},
			raw: t.CreatedAt,
		})
	}
	sort.SliceStable(buf, func(i, j int) bool { return buf[i].raw > buf[j].raw })
	txs := make([]page.MemberTx, len(buf))
	for i, r := range buf {
		txs[i] = r.tx
	}

	return page.MemberDetailProps{
		ID:          m.ID,
		Name:        m.Name,
		Phone:       util.FormatPhone(m.Phone),
		Card:        card,
		Memo:        memo,
		Active:      m.IsActive,
		Balance:     util.FormatKRW(m.Balance),
		BalanceInt:  m.Balance,
		TotalIn:     util.FormatKRW(totalIn),
		TotalOut:    util.FormatKRW(totalOut),
		Memberships: mships,
		RecentTxs:   txs,
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
