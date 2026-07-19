package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// Dashboard — GET /v2/admin?period=today|this-week|this-month|all.
// v1 랜딩 스타일: 기간 필터 + 큰 날짜 + 오늘 차감/충전 + 최근 활동.
func (h *PageHandlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	period := r.URL.Query().Get("period")
	if !isValidPeriod(period) {
		period = "today"
	}
	sinceUTC, untilUTC, _ := periodBounds(period)

	// 트랜잭션 조회 (period 안). ListTransactionsInPeriod 은 until 필수라
	// "all" 인 경우 사실상 상한 없는 문자열로 처리.
	if untilUTC == "" {
		untilUTC = "9999-12-31T23:59:59.999Z"
	}
	txs, err := h.Deps.Services.Queries.ListTransactionsInPeriod(ctx, dbgen.ListTransactionsInPeriodParams{
		CreatedAt:   sinceUTC,
		CreatedAt_2: untilUTC,
	})
	if err != nil {
		slog.Error("v2 dashboard txs", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 차감/충전 합계.
	var chargeTotal, deductTotal int64
	for _, t := range txs {
		switch t.TxType {
		case "charge":
			chargeTotal += t.TxAmount
		case "deduct", "refund":
			deductTotal += t.TxAmount
		}
	}

	// 최근 활동 6건 (트랜잭션 이미 created_at DESC).
	limit := 6
	if len(txs) < limit {
		limit = len(txs)
	}
	recent := make([]page.DashActivity, 0, limit)
	for i := 0; i < limit; i++ {
		t := txs[i]
		name := t.MemberName
		if enc := h.Deps.Services.Enc; enc != nil && name != "" {
			name = enc.Decrypt(name)
		}
		memo := ""
		if t.TxMemo.Valid {
			memo = t.TxMemo.String
		}
		sign := "-"
		if t.TxType == "charge" {
			sign = "+"
		}
		recent = append(recent, page.DashActivity{
			MemberID:   t.MemberID,
			MemberName: name,
			Type:       t.TxType,
			Amount:     sign + util.FormatKRW(t.TxAmount),
			Memo:       memo,
			At:         formatKSTDate(t.TxCreatedAt),
		})
	}

	render(w, r, http.StatusOK, page.Dashboard(page.DashboardProps{
		Period:      period,
		PeriodLabel: strings.ToUpper(periodEyebrow(period)),
		DateTitle:   dashDateTitle(period),
		ChargeTotal: util.FormatKRW(chargeTotal),
		DeductTotal: util.FormatKRW(deductTotal),
		Recent:      recent,
	}))
}

// periodEyebrow — 상단 uppercase eyebrow 라벨.
func periodEyebrow(period string) string {
	switch period {
	case "today":
		return "Today"
	case "this-week":
		return "This Week"
	case "this-month":
		return "This Month"
	case "all":
		return "All Time"
	}
	return "Today"
}

// dashDateTitle — 큰 h1.
// today 는 오늘 날짜, 나머지는 기간 라벨.
func dashDateTitle(period string) string {
	now := time.Now().In(kstLocSales())
	weekday := [...]string{"일요일", "월요일", "화요일", "수요일", "목요일", "금요일", "토요일"}[now.Weekday()]
	switch period {
	case "today":
		return fmt.Sprintf("%d년 %d월 %d일 %s", now.Year(), int(now.Month()), now.Day(), weekday)
	case "this-week":
		return "이번 주"
	case "this-month":
		return fmt.Sprintf("%d년 %d월", now.Year(), int(now.Month()))
	case "all":
		return "전체 기간"
	}
	return fmt.Sprintf("%d년 %d월 %d일", now.Year(), int(now.Month()), now.Day())
}

// formatKSTDate — DB ISO8601 → "7월 18일" (KST).
func formatKSTDate(s string) string {
	if s == "" {
		return ""
	}
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
	t = t.In(kstLocSales())
	return fmt.Sprintf("%d월 %d일", int(t.Month()), t.Day())
}
