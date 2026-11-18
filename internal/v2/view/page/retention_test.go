package page

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestRetentionShowsMeasuredRatesMaturityAndScopedActions(t *testing.T) {
	p := RetentionProps{
		Months: 12, DormantDays: 90, AsOf: "2026-11-17 22:00", HistoryStart: "2026-06-01", LatestVisit: "2026-11-15",
		CurrentRate: "80.0%", CurrentDetail: "재방문 20명 / 방문 25명 · 진행 중",
		MeanGap: "30.5일", GapDetail: "연속 방문 간격 100건 기준", FirstVisitors: "64명",
		DormantCount: 22, NeverVisitedCount: 1, Page: 1, Pages: 2,
		NextURL: "/v2/admin/retention?months=12&absent=90&page=2",
		Rows:    []RetentionMonthRow{{Month: "2026-11", Visitors: "25명", Returning: "20명", VisitDays: "27회", Rate: "80.0%", Gap: "25.0일", RateWidth: "80", Partial: true}},
		Cohorts: []RetentionCohortRow{
			{Month: "2026-06", Size: "10명", Cells: []RetentionCohortCell{{Label: "60.0%", Detail: "6 / 10명", Class: "high"}, {Label: "관찰 중", Detail: "2026-11", Class: "observing"}, {Label: "—", Detail: "2026-12", Class: "future"}}},
			{Month: "2026-11", Size: "5명", Cells: []RetentionCohortCell{{Label: "—", Detail: "2026-12", Class: "future"}}},
		},
		DormantRows: []RetentionDormantRow{{Href: "/v2/admin/members/fixture-id", Name: "검증 <고객>", LastVisit: "2026-06-01", Balance: "₩20,000", DaysAbsent: "169일"}},
	}
	var rendered bytes.Buffer
	if err := Retention(p).Render(context.Background(), &rendered); err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	if strings.Contains(html, "@component.") {
		t.Fatal("template component call was rendered as text")
	}
	for _, want := range []string{
		"월별 재방문율", "첫 방문 월별 유지율", "장기 미방문 고객", "평균 재방문 간격",
		"같은 달에 다시 방문한 고객도 포함", "매달 연속 방문해야 하는 지표는 아닙니다",
		"관찰 중", "모집 진행 중", "60.0%", "6 / 10명", "30.5일", "연속 방문 간격 100건 기준",
		"활성 회원 · 마지막 방문 후 90일 이상 · 분석 기간과 무관",
		`name="months" value="12"`, `name="absent" value="90"`,
		`href="/v2/admin/retention?months=12&amp;absent=90&amp;page=2"`,
		`href="/v2/admin/members/fixture-id"`, "검증 &lt;고객&gt;", "시술 방문 기록이 없는 활성 회원 1명",
		"2026-06-01 ~ 2026-11-15", "실제 첫 방문과 다를 수 있습니다",
		`style="width:80.0%;"`, `aria-label="월별 재방문율 표, 가로 스크롤 가능"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(html, "<고객>") {
		t.Fatal("member name was not escaped")
	}
}

func TestRetentionMetricSetsNumericContrastOnTheValueItself(t *testing.T) {
	// yh-num declares its own ink color, so inheriting white from a dark card
	// does not work. Verify the actual numeric child, not only its parent.
	for _, primary := range []bool{true, false} {
		var rendered bytes.Buffer
		if err := retentionMetric("비율", "88.8%", "검증", primary).Render(context.Background(), &rendered); err != nil {
			t.Fatal(err)
		}
		value := regexp.MustCompile(`<p class="([^"]*)">88\.8%</p>`).FindStringSubmatch(rendered.String())
		if len(value) != 2 || !strings.Contains(value[1], "yh-num") {
			t.Fatalf("numeric value element missing: %s", rendered.String())
		}
		want, excluded := "text-ink-900", "text-sand-50"
		if primary {
			want, excluded = excluded, want
			if !strings.Contains(rendered.String(), "bg-ink-900") {
				t.Fatal("primary card must keep its dark background")
			}
		}
		if !strings.Contains(value[1], want) || strings.Contains(value[1], excluded) {
			t.Fatalf("primary=%t numeric contrast classes %q, want %s without %s", primary, value[1], want, excluded)
		}
	}
}

func TestRetentionIncludesSelectedThreeMonthOption(t *testing.T) {
	var rendered bytes.Buffer
	if err := Retention(RetentionProps{Months: 3, CohortMonths: 6, DormantDays: 60, CurrentRate: "—", MeanGap: "—", FirstVisitors: "0명"}).Render(context.Background(), &rendered); err != nil {
		t.Fatal(err)
	}
	selectHTML := regexp.MustCompile(`(?s)<select id="retention-months"[^>]*>.*?</select>`).FindString(rendered.String())
	options := regexp.MustCompile(`<option value="(3|6|12|24)"[^>]*>`).FindAllStringSubmatch(selectHTML, -1)
	if len(options) != 4 || options[0][1] != "3" || !strings.Contains(options[0][0], "selected") {
		t.Fatalf("three-month option must lead and be selected: %v", options)
	}
	if !strings.Contains(rendered.String(), `name="months" value="3"`) {
		t.Fatal("current absence filter must preserve three-month lookback")
	}
}

func TestRetentionCohortSixMonthsFilterPreservesOtherScopes(t *testing.T) {
	var output bytes.Buffer
	if err := Retention(RetentionProps{Months: 3, CohortMonths: 6, DormantDays: 90, Page: 2}).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	cohorts := regexp.MustCompile(`(?s)<select id="retention-cohorts"[^>]*>.*?</select>`).FindString(html)
	if !strings.Contains(cohorts, `value="6" selected`) || !strings.Contains(html, "최근 6개월의 첫 방문 월") {
		t.Fatal("cohort lookback must independently select six months")
	}
	forms := regexp.MustCompile(`(?s)<form\b[^>]*action="/v2/admin/retention"[^>]*>.*?</form>`).FindAllString(html, -1)
	if len(forms) != 3 {
		t.Fatalf("want scoped controls for activity, cohorts and absence: %d", len(forms))
	}
	for i, form := range forms {
		if !strings.Contains(form, `name="cohorts"`) || !strings.Contains(form, `name="months"`) || !strings.Contains(form, `name="absent"`) {
			t.Errorf("form%d drops another scope", i)
		}
	}
	if !strings.Contains(forms[1], `name="page" value="2"`) {
		t.Fatal("cohort control lost the current action-list page")
	}
}

func TestRetentionEmptyStatesDoNotInventActivity(t *testing.T) {
	p := RetentionProps{Months: 6, DormantDays: 60, CurrentRate: "—", MeanGap: "—", CurrentDetail: "이번 달 방문 기록 없음", GapDetail: "재방문 간격 기록 없음", FirstVisitors: "0명"}
	var rendered bytes.Buffer
	if err := Retention(p).Render(context.Background(), &rendered); err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, want := range []string{"선택한 기간에 시술 방문 기록이 없습니다", "선택한 기간에 기록상 첫 방문 고객이 없습니다", "현재 조건에 해당하는 고객이 없습니다", "아직 집계할 수 있는 시술 방문 기록이 없습니다"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing empty state %q", want)
		}
	}
	if strings.Contains(html, "미방문 고객 페이지") || strings.Contains(html, "<canvas") {
		t.Fatal("empty page invented pagination or depends on a chart runtime")
	}
}
