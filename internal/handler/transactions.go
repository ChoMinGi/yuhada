package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/mingicho/yuhada/internal/httpx"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/view/page"
)

// GET /admin/transactions?period=today|week|month|all&type=charge|deduct&q=…
//
// HTMX 디바운스 검색 시 (HX-Trigger-Name == "q") 거래 리스트 partial 만 swap.
// 그 외 (페이지 진입, 필터 칩 클릭) 풀 페이지 응답.
func (h *PageHandlers) TransactionsPage(w http.ResponseWriter, r *http.Request) {
	p := normalizePeriod(r.URL.Query().Get("period"))
	txType := normalizeTxType(r.URL.Query().Get("type"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	filter := service.TxFilter{Period: p, Type: txType, Q: q}

	rows, err := h.Services.Tx.ListInPeriod(r.Context(), filter)
	if err != nil {
		slog.Error("transactions list", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}

	pRows := make([]page.TxRow, len(rows))
	for i, t := range rows {
		sign := "-"
		if t.TxType == "charge" {
			sign = "+"
		}
		memo := ""
		if t.TxMemo.Valid {
			memo = t.TxMemo.String
		}
		pRows[i] = page.TxRow{
			Type:       t.TxType,
			MemberID:   t.MemberID,
			MemberName: t.MemberName,
			Memo:       memo,
			Amount:     sign + util.FormatKRW(t.TxAmount),
			At:         formatTimeShort(t.TxCreatedAt),
		}
	}

	// HTMX 검색 디바운스 — partial 만 (#tx-list 영역).
	if httpx.IsHTMX(r) && r.Header.Get("HX-Trigger-Name") == "q" {
		httpx.Render(w, r, http.StatusOK, page.TransactionsList(pRows))
		return
	}

	deduct, charge, _ := h.Services.Tx.SumsInPeriod(r.Context(), filter)

	httpx.Render(w, r, http.StatusOK, page.Transactions(page.TransactionsData{
		Period:      string(p),
		Type:        txType,
		Q:           q,
		DeductLabel: periodLabel(p, "차감"),
		ChargeLabel: periodLabel(p, "충전"),
		DeductTotal: util.FormatKRW(deduct),
		ChargeTotal: util.FormatKRW(charge),
		Rows:        pRows,
	}))
}

// normalizeTxType — query string → 유효한 거래 타입. 알 수 없으면 "" (전체).
func normalizeTxType(s string) string {
	if s == "charge" || s == "deduct" {
		return s
	}
	return ""
}
