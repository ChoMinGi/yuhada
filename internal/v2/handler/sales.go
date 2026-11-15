package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mingicho/yuhada/internal/util"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// SalesDashboard — GET /v2/admin/sales?period=today|this-week|this-month|last-month|all.
// 기본 period="this-month".
//
// 기간 계산은 KST 기준으로 하고 UTC ISO8601 로 변환하여 SQL 문자열 비교.
// (created_at 은 strftime('%Y-%m-%dT%H:%M:%fZ','now') 형식 → 사전순 비교가 시간순과 일치.)
func (h *PageHandlers) SalesDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	period := normalizePeriod(r.URL.Query().Get("period"))
	if !isValidPeriod(period) {
		period = "this-month"
	}
	var (
		sinceUTC, untilUTC, label string
		periodFrom, periodTo      string
	)
	if period == "custom" {
		fromStr := r.URL.Query().Get("from")
		toStr := r.URL.Query().Get("to")
		start, endExc, lbl, ok := parseCustomRange(fromStr, toStr)
		if !ok {
			period = "this-month"
			sinceUTC, untilUTC, label = periodBounds(period)
		} else {
			sinceUTC = toUTCIso(start)
			untilUTC = toUTCIso(endExc)
			label = lbl
			periodFrom = start.Format("2006-01-02")
			periodTo = endExc.AddDate(0, 0, -1).Format("2006-01-02") // to 는 포함일이므로 -1
		}
	}
	if period != "custom" {
		sinceUTC, untilUTC, label = periodBounds(period)
	}
	if periodFrom == "" || periodTo == "" {
		today := time.Now().In(kstLoc).Format("2006-01-02")
		if periodFrom == "" {
			periodFrom = today
		}
		if periodTo == "" {
			periodTo = today
		}
	}

	summary, err := h.Deps.Services.Sales.SalesSummary(ctx, sinceUTC, untilUTC)
	if err != nil {
		slog.Error("v2 sales summary", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tierRows, err := h.Deps.Services.Sales.SalesByTier(ctx, sinceUTC, untilUTC)
	if err != nil {
		slog.Error("v2 sales by tier", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	payRows, err := h.Deps.Services.Sales.SalesByPayMethod(ctx, sinceUTC, untilUTC)
	if err != nil {
		slog.Error("v2 sales by pay method", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	recent, err := h.Deps.Services.Sales.RecentSales(ctx, sinceUTC, untilUTC, 20)
	if err != nil {
		slog.Error("v2 sales recent", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	daily, err := h.Deps.Services.Sales.DailyRevenue(ctx, sinceUTC, untilUTC)
	if err != nil {
		slog.Error("v2 sales daily", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	top, err := h.Deps.Services.Sales.TopMembers(ctx, sinceUTC, untilUTC, 5)
	if err != nil {
		slog.Error("v2 sales top members", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	insights, err := h.Deps.Services.Sales.Insights(ctx, sinceUTC, untilUTC, now)
	if err != nil {
		slog.Error("v2 sales insights", "err", err)
		http.Error(w, "현황을 불러오지 못했습니다", http.StatusInternalServerError)
		return
	}

	// 그래프용 데이터 JSON 인코딩 (Chart.js 초기화 스크립트에 임베드).
	dailyLabels, dailyValues, usageValues, granularity, trendRows := buildSalesTrend(daily, insights.DailyUsage, sinceUTC, untilUTC, period, now)
	tierLabels, tierValues := buildTierChart(tierRows)
	payLabels, payValues := buildPayChart(payRows)

	render(w, r, http.StatusOK, page.Sales(page.SalesProps{
		Period:          period,
		PeriodLabel:     label,
		PeriodFrom:      periodFrom,
		PeriodTo:        periodTo,
		SummaryCards:    buildSummaryCards(summary, 0),
		Insights:        buildSalesInsights(insights, dailyLabels, dailyValues, usageValues, granularity, trendRows),
		TierRows:        buildTierRows(tierRows),
		PayRows:         buildPayRows(payRows),
		RecentSales:     buildRecentRows(recent),
		TopMembers:      buildTopMembers(top),
		DailyLabelsJSON: mustJSON(dailyLabels),
		DailyValuesJSON: mustJSON(dailyValues),
		TierChartJSON:   mustJSON(map[string]any{"labels": tierLabels, "values": tierValues}),
		PayChartJSON:    mustJSON(map[string]any{"labels": payLabels, "values": payValues}),
	}))
}

// kstLocSales — KST 로케일 (에러 시 고정 오프셋).
func kstLocSales() *time.Location {
	if l, err := time.LoadLocation("Asia/Seoul"); err == nil {
		return l
	}
	return time.FixedZone("KST", 9*3600)
}

func buildExpiringRows(rows []v2service.ExpiringMembership) ([]page.SalesExpiring, int64) {
	out := make([]page.SalesExpiring, 0, len(rows))
	var total int64
	for _, r := range rows {
		total += r.CreditAmount
		out = append(out, page.SalesExpiring{
			MemberID:     r.MemberID,
			MemberName:   r.MemberName,
			TierLabel:    r.TierLabel,
			PayMethod:    r.PayMethod,
			CreditAmount: util.FormatKRW(r.CreditAmount),
			ExpiresAt:    formatKSTShort(r.ExpiresAt),
		})
	}
	return out, total
}

func buildDormantRows(rows []v2service.DormantMember) ([]page.SalesDormant, int64) {
	out := make([]page.SalesDormant, 0, len(rows))
	var total int64
	for _, r := range rows {
		total += r.Balance
		last := "방문 이력 없음"
		if r.LastTxAt != "" {
			last = formatKSTShort(r.LastTxAt)
		}
		out = append(out, page.SalesDormant{
			MemberID: r.MemberID,
			Name:     r.Name,
			Balance:  util.FormatKRW(r.Balance),
			LastAt:   last,
		})
	}
	return out, total
}

// formatKSTShort — DB ISO8601 UTC → "1월 2일" (KST).
func formatKSTShort(s string) string {
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

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// buildDailyChart — 기간의 시작~끝 KST 날짜를 채우고, 매출 없는 날은 0 으로 정규화.
// 결과: (["07-01","07-02",...], [0, 240000, 0, ...])
func buildDailyChart(daily []v2service.DailyBucket, sinceUTC, untilUTC string) ([]string, []int64) {
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		kst = time.FixedZone("KST", 9*3600)
	}
	since, err := time.Parse("2006-01-02T15:04:05.000Z", sinceUTC)
	if err != nil {
		since = time.Now().UTC().Add(-30 * 24 * time.Hour)
	}
	var until time.Time
	if untilUTC == "" {
		until = time.Now().UTC()
	} else {
		u, uerr := time.Parse("2006-01-02T15:04:05.000Z", untilUTC)
		if uerr != nil {
			until = time.Now().UTC()
		} else {
			until = u
		}
	}
	sinceKST := since.In(kst)
	untilKST := until.In(kst)

	// 판매 있는 날 인덱스.
	idx := make(map[string]int64, len(daily))
	for _, d := range daily {
		idx[d.Date] = d.Revenue
	}

	// 축의 길이가 너무 길면 (전체 · all) 최근 60일만.
	start := time.Date(sinceKST.Year(), sinceKST.Month(), sinceKST.Day(), 0, 0, 0, 0, kst)
	end := time.Date(untilKST.Year(), untilKST.Month(), untilKST.Day(), 0, 0, 0, 0, kst)
	if end.Before(start) {
		end = start
	}
	maxDays := 60
	if delta := int(end.Sub(start).Hours()/24) + 1; delta > maxDays {
		start = end.AddDate(0, 0, -(maxDays - 1))
	}

	labels := []string{}
	values := []int64{}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		labels = append(labels, d.Format("01-02"))
		values = append(values, idx[key])
	}
	return labels, values
}

// buildTierChart — 등급별 판매 건수 pie 데이터.
func buildTierChart(rows []v2service.TierRow) ([]string, []int) {
	if len(rows) == 0 {
		return []string{}, []int{}
	}
	labels := make([]string, 0, len(rows))
	values := make([]int, 0, len(rows))
	for _, r := range rows {
		lbl := r.TierLabel
		if r.PayMethod == "card" {
			lbl += " · 카드"
		} else if r.PayMethod == "cash" {
			lbl += " · 현금"
		}
		labels = append(labels, lbl)
		values = append(values, r.Count)
	}
	return labels, values
}

// buildPayChart — 모든 판매의 결제수단 건수. 미상 그룹 포함.
func buildPayChart(rows []v2service.PayMethodRow) ([]string, []int) {
	if len(rows) == 0 {
		return []string{}, []int{}
	}
	labels := make([]string, 0, len(rows))
	values := make([]int, 0, len(rows))
	for _, r := range rows {
		if r.PayMethod == "card" {
			labels = append(labels, "카드")
		} else if r.PayMethod == "cash" {
			labels = append(labels, "현금")
		} else {
			labels = append(labels, "결제수단 미상")
		}
		values = append(values, r.Count)
	}
	return labels, values
}

// ─────────────────────────────────────────────
// 기간 계산 (KST)
// ─────────────────────────────────────────────

func normalizePeriod(p string) string {
	switch p {
	case "week":
		return "this-week"
	case "month":
		return "this-month"
	}
	return p
}

func isValidPeriod(p string) bool {
	switch p {
	case "today", "this-week", "this-month", "last-month", "all", "custom":
		return true
	}
	return false
}

// parseCustomRange — from/to (YYYY-MM-DD, KST 기준) 파싱.
// 성공 시 (start 00:00 KST, end 다음날 00:00 KST, label, ok=true).
// end < start 이면 스왑. 미래 end 는 그대로 둠 (사용자 의도 존중).
func parseCustomRange(fromStr, toStr string) (start, endExclusive time.Time, label string, ok bool) {
	from, err1 := time.ParseInLocation("2006-01-02", strings.TrimSpace(fromStr), kstLoc)
	to, err2 := time.ParseInLocation("2006-01-02", strings.TrimSpace(toStr), kstLoc)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, "", false
	}
	if to.Before(from) {
		from, to = to, from
	}
	endExclusive = to.AddDate(0, 0, 1) // to 일 전체 포함 → 다음날 00:00 배타
	return from, endExclusive, customPeriodLabel(from, to), true
}

// customPeriodLabel — "6월 1일 – 6월 30일" (동일 년) / "2025년 12월 1일 – 2026년 1월 5일" (다른 년).
func customPeriodLabel(from, to time.Time) string {
	if from.Year() == to.Year() {
		if from.Year() == time.Now().In(kstLoc).Year() {
			return fmt.Sprintf("%d월 %d일 – %d월 %d일", from.Month(), from.Day(), to.Month(), to.Day())
		}
		return fmt.Sprintf("%d년 %d월 %d일 – %d월 %d일", from.Year(), from.Month(), from.Day(), to.Month(), to.Day())
	}
	return fmt.Sprintf("%d년 %d월 %d일 – %d년 %d월 %d일",
		from.Year(), from.Month(), from.Day(),
		to.Year(), to.Month(), to.Day())
}

// periodBounds — period 문자열 → (sinceUTC ISO8601, untilUTC ISO8601, 한글 라벨).
// untilUTC 가 "" 이면 상한 없음.
func periodBounds(period string) (string, string, string) {
	nowKST := time.Now().In(kstLoc)
	switch period {
	case "today":
		start := time.Date(nowKST.Year(), nowKST.Month(), nowKST.Day(), 0, 0, 0, 0, kstLoc)
		return toUTCIso(start), "", "오늘"
	case "this-week":
		// ISO week: 월요일 시작.
		wd := int(nowKST.Weekday()) // Sunday=0 .. Saturday=6
		daysFromMonday := wd - 1
		if daysFromMonday < 0 {
			daysFromMonday = 6 // Sunday → 지난 월요일부터 6일 전
		}
		monday := time.Date(nowKST.Year(), nowKST.Month(), nowKST.Day()-daysFromMonday, 0, 0, 0, 0, kstLoc)
		return toUTCIso(monday), "", "이번 주"
	case "last-month":
		// 지난 달 1일 00:00 KST ~ 이번 달 1일 00:00 KST (배타적 상한).
		thisMonthStart := time.Date(nowKST.Year(), nowKST.Month(), 1, 0, 0, 0, 0, kstLoc)
		lastMonthStart := thisMonthStart.AddDate(0, -1, 0)
		return toUTCIso(lastMonthStart), toUTCIso(thisMonthStart), "지난 달"
	case "all":
		epoch := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		return toUTCIso(epoch), "", "전체"
	default: // "this-month"
		start := time.Date(nowKST.Year(), nowKST.Month(), 1, 0, 0, 0, 0, kstLoc)
		return toUTCIso(start), "", "이번 달"
	}
}

// toUTCIso — DB 저장 포맷(밀리초 3자리 UTC ISO8601)에 사전순 비교 가능한 형태로.
// strftime('%Y-%m-%dT%H:%M:%fZ','now') → "2026-07-12T08:39:47.123Z".
// 하한이 정각 00:00:00 이라 밀리초 000 으로 붙여 비교하면 이후의 모든 row 포함.
func toUTCIso(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// ─────────────────────────────────────────────
// view builders (raw → formatted)
// ─────────────────────────────────────────────

func buildSummaryCards(s v2service.SalesSummary, usedPct int) page.SalesSummaryCards {
	return page.SalesSummaryCards{
		RealRevenue:   util.FormatKRW(s.RealRevenue),
		CreditTotal:   util.FormatKRW(s.CreditTotal),
		BonusCost:     util.FormatKRW(s.BonusCost),
		TxCount:       fmt.Sprintf("%d건", s.TxCount),
		AvgTicket:     util.FormatKRW(s.AvgTicket),
		ActiveMembers: fmt.Sprintf("%d명", s.ActiveMembers),
		PlanShare:     fmt.Sprintf("%d%%", (s.PlanShareBP+50)/100), // basis points → 반올림 %
		RepeatMembers: fmt.Sprintf("%d명", s.RepeatMembers),
		CreditUsedPct: usedPct,
	}
}

// buildTopMembers — 서비스 TopMember → 페이지 SalesTopMember (포맷 완료).
// rank 는 1-based (index+1).
func buildTopMembers(rows []v2service.TopMember) []page.SalesTopMember {
	out := make([]page.SalesTopMember, 0, len(rows))
	for i, r := range rows {
		out = append(out, page.SalesTopMember{
			MemberID:   r.MemberID,
			Name:       r.Name,
			TotalPrice: util.FormatKRW(r.TotalPrice),
			Count:      fmt.Sprintf("%d건", r.Count),
			Rank:       i + 1,
		})
	}
	return out
}

func buildTierRows(rows []v2service.TierRow) []page.SalesTierRow {
	out := make([]page.SalesTierRow, 0, len(rows))
	for _, r := range rows {
		row := page.SalesTierRow{
			TierLabel:   r.TierLabel,
			PayMethod:   r.PayMethod,
			IsGeneric:   r.IsGeneric,
			CountLabel:  fmt.Sprintf("%d건", r.Count),
			TotalPrice:  util.FormatKRW(r.TotalPrice),
			TotalCredit: util.FormatKRW(r.TotalCredit),
		}
		if !r.IsGeneric && r.TotalBonus > 0 {
			row.TotalBonus = util.FormatKRW(r.TotalBonus)
			row.HasBonus = true
		}
		out = append(out, row)
	}
	return out
}

func buildPayRows(rows []v2service.PayMethodRow) []page.SalesPayMethodRow {
	var total int64
	for _, r := range rows {
		total += r.TotalPrice
	}
	out := make([]page.SalesPayMethodRow, 0, len(rows))
	for _, r := range rows {
		pct := 0
		if total > 0 {
			pct = int(r.TotalPrice * 100 / total)
		}
		out = append(out, page.SalesPayMethodRow{
			PayMethod:  r.PayMethod,
			CountLabel: fmt.Sprintf("%d건", r.Count),
			TotalPrice: util.FormatKRW(r.TotalPrice),
			Percent:    pct,
		})
	}
	return out
}

func buildRecentRows(rows []v2service.RecentSale) []page.SalesRecentRow {
	out := make([]page.SalesRecentRow, 0, len(rows))
	for _, r := range rows {
		row := page.SalesRecentRow{
			MemberID:   r.MemberID,
			MemberName: r.MemberName,
			Amount:     "+" + util.FormatKRW(r.Amount),
			At:         formatKST(r.CreatedAt),
			Relative:   relativeKST(r.CreatedAt),
			TierLabel:  r.TierLabel,
			PayMethod:  r.PayMethod,
			IsGeneric:  r.TierLabel == "",
		}
		if r.CreditAmount != r.Amount {
			row.Credit = "적립 " + util.FormatKRW(r.CreditAmount)
		}
		out = append(out, row)
	}
	return out
}

// relativeKST — "3분 전", "1시간 전" 같은 상대 시각.
// 파싱 실패 시 빈 문자열.
func relativeKST(s string) string {
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
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "방금 전"
	case d < time.Hour:
		return fmt.Sprintf("%d분 전", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d일 전", int(d.Hours()/24))
	default:
		return ""
	}
}
