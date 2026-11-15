package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	legacyservice "github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// Dashboard — GET /v2/admin?period=today|this-week|this-month|all.
// v1 랜딩 스타일: 기간 필터 + 큰 날짜 + 오늘 차감/충전 + 최근 활동.
func (h *PageHandlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	period := normalizePeriod(r.URL.Query().Get("period"))
	legacyPeriod := legacyservice.PeriodToday
	switch period {
	case "this-week":
		legacyPeriod = legacyservice.PeriodWeek
	case "this-month":
		legacyPeriod = legacyservice.PeriodMonth
	case "all":
		legacyPeriod = legacyservice.PeriodAll
	default:
		period = "today"
	}
	// Keep the original dashboard period boundaries and refund semantics.
	snapshot, err := h.legacyServices().Stats.Dashboard(ctx, legacyPeriod)
	if err != nil {
		slog.Error("v2 dashboard totals", "err", err)
		http.Error(w, "합계를 불러오지 못했습니다", 500)
		return
	}
	chargeTotal, deductTotal := snapshot.ChargeTotal, snapshot.DeductTotal
	txs, err := h.Deps.Services.Queries.ListRecentTransactions(ctx)
	if err != nil {
		http.Error(w, "최근 활동을 불러오지 못했습니다", 500)
		return
	}
	// 선택 기간과 별개로 가장 최근 활동 8건을 보여준다.
	limit := 8
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
			At:         formatKST(t.TxCreatedAt),
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
