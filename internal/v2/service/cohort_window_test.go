package service

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestRetentionCohortWindowDefaultsToSixMonthsIndependentlyOfThreeMonthAnalysis(t *testing.T) {
	db, all, _ := sharedFixture(t)
	owner := insightMember(t, all, 9200, 0, true)
	prior := insightMember(t, all, 9201, 0, true)
	june := insightMember(t, all, 9202, 0, true)
	july := insightMember(t, all, 9203, 0, true)
	august := insightMember(t, all, 9204, 0, false)
	september := insightMember(t, all, 9205, 0, true)
	october := insightMember(t, all, 9206, 0, true)
	november := insightMember(t, all, 9207, 0, true)
	for i, fixture := range []struct{ member, day string }{
		{owner, "2026-06-02"}, {owner, "2026-09-02"}, {owner, "2026-11-02"},
		{prior, "2026-05-01"}, {prior, "2026-09-05"},
		{june, "2026-06-01"}, {june, "2026-07-01"}, {june, "2026-09-01"}, {june, "2026-11-01"},
		{july, "2026-07-01"}, {july, "2026-08-01"}, {july, "2026-10-01"},
		{august, "2026-08-01"}, {august, "2026-11-01"},
		{september, "2026-09-01"}, {september, "2026-10-01"},
		{october, "2026-10-01"}, {october, "2026-11-01"},
		{november, "2026-11-01"},
	} {
		insightTx(t, db, fmt.Sprintf("cohort-window-%02d", i), fixture.member, "deduct", 1000, retentionISO(retentionDay(t, fixture.day)), "")
	}
	insightReadOnly(t, db)
	scoped := New(db, all.Enc, owner)
	now := retentionDay(t, "2026-11-18")
	got, err := scoped.Sales.Retention(context.Background(), RetentionOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if got.CohortMonths != 6 || len(got.Cohorts) != 6 {
		t.Fatalf("default independent cohort window: %+v", got.Cohorts)
	}
	for i, cohort := range got.Cohorts {
		wantMonth := fmt.Sprintf("2026-%02d", i+6)
		if cohort.Month != wantMonth || cohort.Size != 1 || len(cohort.Cells) != 6 {
			t.Fatalf("missing earlier cohort or owner leaked at %d: %+v", i, cohort)
		}
	}
	if len(got.Months) != 3 || got.Months[0].Month != "2026-09" || got.FirstVisitorCount != 3 || got.ReturningCustomerCount != 6 || got.GapCount != 7 || math.Abs(got.MeanGapDays-464.0/7) > .000001 {
		t.Fatalf("cohort lookback expanded the analysis metrics: %+v", got)
	}
	for i, want := range [][2]int{{3, 2}, {3, 2}, {4, 3}} {
		if got.Months[i].Visitors != want[0] || got.Months[i].Returning != want[1] {
			t.Fatalf("analysis month %d changed: %+v", i, got.Months[i])
		}
	}
	wantJune := []RetentionCell{{1, 1, "complete"}, {2, 0, "complete"}, {3, 1, "complete"}, {4, 0, "complete"}, {5, 1, "observing"}, {6, 0, "future"}}
	if !reflect.DeepEqual(got.Cohorts[0].Cells, wantJune) || got.Cohorts[4].Cells[0] != (RetentionCell{1, 1, "observing"}) || got.Cohorts[5].Cells[0].Status != "future" {
		t.Fatalf("exact month activity or maturity changed: %+v", got.Cohorts)
	}
	narrow, err := scoped.Sales.Retention(context.Background(), RetentionOptions{Now: now, Months: 3, CohortMonths: 3})
	if err != nil {
		t.Fatal(err)
	}
	if narrow.CohortMonths != 3 || len(narrow.Cohorts) != 3 || narrow.Cohorts[0].Month != "2026-09" || !reflect.DeepEqual(narrow.Months, got.Months) || narrow.GapCount != got.GapCount || narrow.MeanGapDays != got.MeanGapDays || narrow.FirstVisitorCount != got.FirstVisitorCount || narrow.DormantCount != got.DormantCount || narrow.NeverVisitedCount != got.NeverVisitedCount {
		t.Fatalf("cohort filter changed another section: narrow=%+v wide=%+v", narrow, got)
	}
	wide, err := scoped.Sales.Retention(context.Background(), RetentionOptions{Now: now, Months: 3, CohortMonths: 12})
	if err != nil || wide.CohortMonths != 12 || len(wide.Cohorts) != 7 || wide.Cohorts[0].Month != "2026-05" || wide.Cohorts[0].Size != 1 {
		t.Fatalf("full history first visit was reclassified at filter boundary: %+v %v", wide, err)
	}
	invalid, err := scoped.Sales.Retention(context.Background(), RetentionOptions{Now: now, Months: 3, CohortMonths: -1})
	if err != nil || invalid.CohortMonths != 6 || !reflect.DeepEqual(invalid.Cohorts, got.Cohorts) {
		t.Fatalf("invalid cohort window did not use default six: %+v %v", invalid, err)
	}
	if tableCount(t, db, "members") != 8 || tableCount(t, db, "transactions") != 19 {
		t.Fatal("cohort query modified source rows")
	}
}
