package handler

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mingicho/yuhada/internal/db/dbgen"
	"github.com/mingicho/yuhada/internal/service"
)

// DebugHandlers — 개발·검증용 JSON endpoint 모음.
// Step 4에서 RequireAdmin 뒤로 이동 예정.
type DebugHandlers struct {
	Services *service.Services
}

// ─────────────────────────────────────────────
// GET /debug/members — 회원 목록 (검색 + 정렬)
// ─────────────────────────────────────────────
func (h *DebugHandlers) ListMembers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	sortKey := service.SortKey(r.URL.Query().Get("sort"))
	members, err := h.Services.Member.Search(r.Context(), q, sortKey)
	if err != nil {
		httpJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]memberDTO, len(members))
	for i, m := range members {
		out[i] = toMemberDTO(m)
	}
	httpJSON(w, http.StatusOK, map[string]any{
		"count":   len(out),
		"members": out,
	})
}

// ─────────────────────────────────────────────
// GET /debug/dashboard — 대시보드 집계
// ─────────────────────────────────────────────
func (h *DebugHandlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	snap, err := h.Services.Stats.Dashboard(r.Context(), service.PeriodToday)
	if err != nil {
		httpJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	httpJSON(w, http.StatusOK, snap)
}

// ─────────────────────────────────────────────
// GET /debug/recent — 최근 거래 (회원명 join)
// ─────────────────────────────────────────────
func (h *DebugHandlers) RecentTransactions(w http.ResponseWriter, r *http.Request) {
	txs, err := h.Services.Tx.ListRecent(r.Context(), 10)
	if err != nil {
		httpJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]recentTxDTO, len(txs))
	for i, t := range txs {
		out[i] = recentTxDTO{
			ID:           t.TxID,
			Type:         t.TxType,
			Amount:       t.TxAmount,
			Memo:         nullStrOrEmpty(t.TxMemo),
			BalanceAfter: t.TxBalanceAfter,
			CreatedAt:    t.TxCreatedAt,
			MemberID:     t.MemberID,
			MemberName:   t.MemberName,
		}
	}
	httpJSON(w, http.StatusOK, map[string]any{
		"count": len(out),
		"txs":   out,
	})
}

// ─────────────────────────────────────────────
// POST /debug/charge?id=...&amount=...&memo=...
// wallet 트랜잭션 동작 검증용.
// ─────────────────────────────────────────────
func (h *DebugHandlers) Charge(w http.ResponseWriter, r *http.Request) {
	h.walletOp(w, r, "charge")
}
func (h *DebugHandlers) Deduct(w http.ResponseWriter, r *http.Request) {
	h.walletOp(w, r, "deduct")
}

func (h *DebugHandlers) walletOp(w http.ResponseWriter, r *http.Request, op string) {
	memberID := r.URL.Query().Get("id")
	amtStr := r.URL.Query().Get("amount")
	memo := r.URL.Query().Get("memo")
	amt, err := strconv.ParseInt(amtStr, 10, 64)
	if err != nil || amt <= 0 {
		httpJSONErr(w, http.StatusBadRequest, err)
		return
	}

	var res service.WalletResult
	switch op {
	case "charge":
		res, err = h.Services.Wallet.Charge(r.Context(), memberID, amt, memo, "")
	case "deduct":
		res, err = h.Services.Wallet.Deduct(r.Context(), memberID, amt, memo, "")
	}
	if err != nil {
		httpJSONErr(w, http.StatusBadRequest, err)
		return
	}
	httpJSON(w, http.StatusOK, map[string]any{
		"op":          op,
		"member":      toMemberDTO(res.Member),
		"new_balance": res.NewBalance,
		"tx_id":       res.Transaction.ID,
	})
}

// ─────────────────────────────────────────────
// DTOs — sql.NullString 풀어서 평탄한 JSON 내보내기
// ─────────────────────────────────────────────

type memberDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	CardUUID    string `json:"cardUuid,omitempty"`
	Balance     int64  `json:"balance"`
	Memo        string `json:"memo,omitempty"`
	IsActive    bool   `json:"isActive"`
	CreatedAt   string `json:"createdAt"`
	KakaoUserID string `json:"kakaoUserId,omitempty"`
}

type recentTxDTO struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Amount       int64  `json:"amount"`
	Memo         string `json:"memo,omitempty"`
	BalanceAfter int64  `json:"balanceAfter"`
	CreatedAt    string `json:"createdAt"`
	MemberID     string `json:"memberId"`
	MemberName   string `json:"memberName"`
}

func toMemberDTO(m dbgen.Member) memberDTO {
	return memberDTO{
		ID:          m.ID,
		Name:        m.Name,
		Phone:       m.Phone,
		CardUUID:    nullStrOrEmpty(m.CardUuid),
		Balance:     m.Balance,
		Memo:        nullStrOrEmpty(m.Memo),
		IsActive:    m.IsActive,
		CreatedAt:   m.CreatedAt,
		KakaoUserID: nullStrOrEmpty(m.KakaoUserID),
	}
}

func nullStrOrEmpty(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func httpJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("json encode", "err", err)
	}
}

func httpJSONErr(w http.ResponseWriter, status int, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	httpJSON(w, status, map[string]string{"error": msg})
}
