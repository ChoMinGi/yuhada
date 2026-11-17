package page

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSalesSeparatesPeriodActivityFromCurrentBalances(t *testing.T) {
	props := SalesProps{
		Period: "this-month", PeriodLabel: "이번 달",
		SummaryCards: SalesSummaryCards{RealRevenue: "₩1,590,000", TxCount: "6건"},
		Insights: SalesInsightsProps{
			UsageTotal: "₩1,744,000", VisitorCount: "25명", VisitCount: "27건",
			FirstCount: 6, FirstAmount: "₩1,590,000", RepeatAmount: "₩0", FirstPercent: 100,
			CurrentBalance: "₩10,044,000", CurrentMemberCount: 65,
			BalanceBuckets:  []SalesBalanceBucket{{Label: "0원", Count: 6, Amount: "₩0", Percent: 9}},
			LowBalanceCount: 16,
			LowBalance:      []SalesBalanceMember{{MemberID: "fixture-member", Name: "검증 고객", Balance: "₩20,000", LastVisit: "11월 15일"}},
			TrendLabelsJSON: `["11월 1일"]`, TrendSalesJSON: `[1590000]`, TrendUsageJSON: `[1744000]`, TrendGranularity: "일별",
			TrendRows: []SalesTrendRow{{Label: "11월 1일", Sales: "₩1,590,000", Usage: "₩1,744,000"}},
		},
	}
	var rendered bytes.Buffer
	if err := Sales(props).Render(context.Background(), &rendered); err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	if strings.Contains(html, "@component.") || strings.Contains(html, "@layout.") {
		t.Fatal("template component call was rendered as text")
	}
	for _, expected := range []string{
		"시술 사용액", "시술 차감액 · 결제 매출 아님", "25명", "중복 제외 · 차감 27건",
		"고객 수가 아닌 충전 건수 기준", "고객 이탈률을 뜻하지 않습니다.",
		"현재 기준 · 기간 필터와 무관", "₩10,044,000", "고객 65명 기준",
		"잔액 5만원 이하 · 최근 60일 안에 시술한 고객", "/v2/admin/members/fixture-member",
		"기간별 수치 보기", `<td class="py-2 pl-3 whitespace-nowrap">₩1,744,000</td>`,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("missing %q", expected)
		}
	}
	for _, removed := range []string{"등급별 판매 비율", "잔액 적립 총액", "보너스 비용", "이번 달 만료 예정", "sales-tier-chart", "sales-pay-chart"} {
		if strings.Contains(html, removed) {
			t.Errorf("obsolete dashboard element %q remains", removed)
		}
	}
	if got := salesRechargeDisplay(props.Insights); got != "0%" {
		t.Errorf("first-only period should show 0%% recharge, got %s", got)
	}
}

func TestSalesEmptyPeriodDoesNotInventRatiosOrPlanDetails(t *testing.T) {
	if got := salesRechargeDisplay(SalesInsightsProps{}); got != "—" {
		t.Fatalf("empty period ratio = %q", got)
	}
	if salesHasPlanRows([]SalesTierRow{{IsGeneric: true}}) {
		t.Fatal("generic credits must not open plan details")
	}
	if !salesHasPlanRows([]SalesTierRow{{IsGeneric: false}}) {
		t.Fatal("recorded plan sales should remain available")
	}
}
