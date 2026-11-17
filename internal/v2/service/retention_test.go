package service

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"
)

func retentionDay(t *testing.T, day string) time.Time {
	t.Helper()
	date, err := time.ParseInLocation("2006-01-02", day, retentionKST)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func retentionISO(at time.Time) string { return at.UTC().Format("2006-01-02T15:04:05.000Z") }

func TestRetentionDistinctVisitsKSTHistoryAndInactiveCustomers(t *testing.T) {
	db, services, _ := sharedFixture(t)
	now := insightTime(t, "2026-03-15T01:00:00.000Z")
	a := insightMember(t, services, 101, 10000, true)
	b := insightMember(t, services, 102, 20000, false)
	c := insightMember(t, services, 103, 30000, true)
	d := insightMember(t, services, 104, 40000, true)
	e := insightMember(t, services, 105, 50000, true)
	f := insightMember(t, services, 106, 60000, true)
	g := insightMember(t, services, 107, 70000, true)
	for i, fixture := range []struct{ member, day string }{
		{a, "2025-09-30"}, {a, "2026-01-10"}, {a, "2026-01-10"}, {a, "2026-02-01"}, {a, "2026-03-01"},
		{b, "2026-01-15"}, {b, "2026-01-20"}, {f, "2026-01-01"},
	} {
		insightTx(t, db, fmt.Sprintf("retention-%d", i), fixture.member, "deduct", 1000, retentionISO(retentionDay(t, fixture.day).Add(12*time.Hour)), "")
	}
	// Two adjacent UTC timestamps cross KST midnight, then a later same-day
	// transaction must not become a third visit or a zero-day interval.
	insightTx(t, db, "c-before-midnight", c, "deduct", 1000, "2026-02-28T14:59:59.999Z", "")
	insightTx(t, db, "c-midnight", c, "deduct", 1000, "2026-02-28T15:00:00.000Z", "")
	insightTx(t, db, "c-same-day", c, "deduct", 1000, "2026-03-01T03:00:00.000Z", "")
	insightTx(t, db, "d-at-now", d, "deduct", 1000, retentionISO(now), "")
	insightTx(t, db, "d-future", d, "deduct", 1000, retentionISO(now.Add(24*time.Hour)), "")
	insightTx(t, db, "g-future-only", g, "deduct", 1000, retentionISO(now.Add(time.Millisecond)), "")
	insightTx(t, db, "e-only-charge", e, "charge", 1000, "2026-01-01T00:00:00.000Z", "")
	insightTx(t, db, "e-only-refund", e, "refund", 1000, "2026-02-01T00:00:00.000Z", "")
	insightReadOnly(t, db)
	beforeTransactions := tableCount(t, db, "transactions")
	got, err := services.Sales.Retention(context.Background(), RetentionOptions{Now: now, Months: 6, DormantDays: 60, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 6 || got.Months[0].Month != "2025-10" || got.Months[5].Month != "2026-03" || !got.Months[5].Partial || got.Months[4].Partial {
		t.Fatalf("wrong selected months: %+v", got.Months)
	}
	if got.HistoryStart != "2025-09-30" || got.LatestVisit != "2026-03-15" {
		t.Fatalf("history lost first/last observations: %+v", got)
	}
	if got.GapCount != 5 || math.Abs(got.MeanGapDays-31.6) > .00001 || got.ReturningCustomerCount != 3 || got.FirstVisitorCount != 4 {
		t.Fatalf("incorrect selected-history gap denominator or first visits: %+v", got)
	}
	// A previous visit before the selected window still makes A returning.
	jan, feb, mar := got.Months[3], got.Months[4], got.Months[5]
	if jan.Visitors != 3 || jan.Returning != 2 || jan.VisitDays != 4 || jan.GapCount != 2 || jan.MeanGapDays != 53.5 {
		t.Fatalf("January distinct customers/days/history incorrect: %+v", jan)
	}
	if feb.Visitors != 2 || feb.Returning != 1 || feb.VisitDays != 2 || feb.GapCount != 1 || feb.MeanGapDays != 22 {
		t.Fatalf("February month boundary incorrect: %+v", feb)
	}
	if mar.Visitors != 3 || mar.Returning != 2 || mar.VisitDays != 3 || mar.GapCount != 2 || mar.MeanGapDays != 14.5 {
		t.Fatalf("March dedup/future handling incorrect: %+v", mar)
	}
	if got.DormantCount != 1 || len(got.DormantMembers) != 1 || got.DormantMembers[0].MemberID != f || got.DormantMembers[0].DaysAbsent != 73 || got.DormantMembers[0].Name != "insight fixture 106" || got.DormantMembers[0].Balance != 60000 || got.NeverVisitedCount != 2 {
		t.Fatalf("active dormant queue or never-visited population incorrect: %+v", got)
	}
	if len(got.Cohorts) != 3 || got.Cohorts[0].Month != "2026-01" || got.Cohorts[0].Size != 2 || got.Cohorts[1].Size != 1 || got.Cohorts[2].Size != 1 {
		t.Fatalf("cohorts exclude inactive or conflate member creation with first visit: %+v", got.Cohorts)
	}
	if got.Cohorts[0].Cells[0] != (RetentionCell{Offset: 1, Returning: 0, Status: "complete"}) || got.Cohorts[0].Cells[1] != (RetentionCell{Offset: 2, Returning: 0, Status: "observing"}) || got.Cohorts[0].Cells[2].Status != "future" || got.Cohorts[1].Cells[0] != (RetentionCell{Offset: 1, Returning: 1, Status: "observing"}) {
		t.Fatalf("cohort observation maturity incorrect: %+v", got.Cohorts)
	}
	if tableCount(t, db, "transactions") != beforeTransactions || tableCount(t, db, "members") != 7 || tableCount(t, db, "v2_transactions") != 1 {
		t.Fatal("read-only retention changed a ledger")
	}
}

func TestRetentionCohortsAreExactCalendarMonthActivityNotContinuousSurvival(t *testing.T) {
	db, services, _ := sharedFixture(t)
	a := insightMember(t, services, 110, 0, true)
	b := insightMember(t, services, 111, 0, true)
	for i, fixture := range []struct{ member, day string }{
		{a, "2026-01-01"}, {a, "2026-03-01"}, {a, "2026-03-20"}, {a, "2026-04-01"},
		{b, "2026-01-31"}, {b, "2026-02-01"},
	} {
		insightTx(t, db, fmt.Sprintf("cohort-%d", i), fixture.member, "deduct", 1000, retentionISO(retentionDay(t, fixture.day)), "")
	}
	insightReadOnly(t, db)
	got, err := services.Sales.Retention(context.Background(), RetentionOptions{Now: retentionDay(t, "2026-04-15"), Months: 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cohorts) != 1 || got.Cohorts[0].Size != 2 {
		t.Fatalf("cohort denominator: %+v", got.Cohorts)
	}
	want := []RetentionCell{{1, 1, "complete"}, {2, 1, "complete"}, {3, 1, "observing"}, {4, 0, "future"}, {5, 0, "future"}, {6, 0, "future"}}
	if !reflect.DeepEqual(got.Cohorts[0].Cells, want) {
		t.Fatalf("skipped-month reappearance or duplicate month counted incorrectly: got %+v want %+v", got.Cohorts[0].Cells, want)
	}
}

func TestRetentionDormantThresholdCalendarDaysAndFullPagination(t *testing.T) {
	db, services, _ := sharedFixture(t)
	now := insightTime(t, "2026-03-15T14:59:59.999Z") // End of March 15 KST.
	for i := 0; i < 23; i++ {
		member := insightMember(t, services, 200+i, int64(i*1000), true)
		last := retentionDay(t, "2026-03-15").AddDate(0, 0, -(60 + i))
		insightTx(t, db, fmt.Sprintf("dormant-%d", i), member, "deduct", 1000, retentionISO(last.Add(23*time.Hour+59*time.Minute)), "")
	}
	for i, fixture := range []struct {
		days   int
		active bool
	}{{59, true}, {100, false}} {
		member := insightMember(t, services, 300+i, 0, fixture.active)
		last := retentionDay(t, "2026-03-15").AddDate(0, 0, -fixture.days)
		insightTx(t, db, fmt.Sprintf("dormant-exclude-%d", i), member, "deduct", 1000, retentionISO(last), "")
	}
	insightReadOnly(t, db)
	first, err := services.Sales.Retention(context.Background(), RetentionOptions{Now: now, Months: 6, DormantDays: 60, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := services.Sales.Retention(context.Background(), RetentionOptions{Now: now, Months: 24, DormantDays: 60, Page: 999999})
	if err != nil {
		t.Fatal(err)
	}
	if first.DormantCount != 23 || first.DormantPages != 2 || first.DormantPage != 1 || len(first.DormantMembers) != 20 || first.DormantMembers[0].DaysAbsent != 82 || second.DormantCount != 23 || second.DormantPage != 2 || len(second.DormantMembers) != 3 || second.DormantMembers[2].DaysAbsent != 60 {
		t.Fatalf("threshold/page truncation: first=%+v second=%+v", first, second)
	}
	seen := map[string]bool{}
	for _, member := range append(first.DormantMembers, second.DormantMembers...) {
		if seen[member.MemberID] || member.Name == "" {
			t.Fatal("duplicate member or missing decrypted name across pages")
		}
		seen[member.MemberID] = true
	}
	ninety, err := services.Sales.Retention(context.Background(), RetentionOptions{Now: now, Months: 6, DormantDays: 90})
	if err != nil || ninety.DormantCount != 0 || ninety.DormantPages != 1 || len(ninety.DormantMembers) != 0 || ninety.NeverVisitedCount != 0 {
		t.Fatalf("90-day queue or historical-filter coupling: %+v %v", ninety, err)
	}
}

func TestRetentionEmptyHistoryAndOptionNormalization(t *testing.T) {
	db, services, _ := sharedFixture(t)
	member := insightMember(t, services, 400, 100000, true)
	insightMember(t, services, 401, 0, false)
	insightTx(t, db, "not-a-visit", member, "charge", 100000, "2026-01-01T00:00:00.000Z", "")
	insightReadOnly(t, db)
	got, err := services.Sales.Retention(context.Background(), RetentionOptions{Now: retentionDay(t, "2026-03-01"), Months: -1, DormantDays: 7, Page: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 12 || got.DormantDays != 60 || got.DormantPage != 1 || got.DormantPages != 1 || got.DormantCount != 0 || got.GapCount != 0 || got.MeanGapDays != 0 || got.ReturningCustomerCount != 0 || got.FirstVisitorCount != 0 || got.NeverVisitedCount != 1 || got.HistoryStart != "" || got.LatestVisit != "" || len(got.Cohorts) != 0 || len(got.DormantMembers) != 0 {
		t.Fatalf("empty/normalized result: %+v", got)
	}
	for _, month := range got.Months {
		if month.Visitors != 0 || month.Returning != 0 || month.GapCount != 0 || month.MeanGapDays != 0 || math.IsNaN(month.MeanGapDays) {
			t.Fatalf("empty month invented measurements: %+v", month)
		}
	}
}

func TestRetentionRejectsNonPositiveDeductionsAndSupportsCancellation(t *testing.T) {
	db, services, _ := sharedFixture(t)
	member := insightMember(t, services, 410, 0, true)
	// The canonical schema already forbids these amounts. Disable that check
	// only in this disposable fixture to prove the read filter is defensive.
	if _, err := db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	insightTx(t, db, "zero-deduct", member, "deduct", 0, "2026-01-01T00:00:00.000Z", "")
	insightTx(t, db, "negative-deduct", member, "deduct", -1000, "2026-02-01T00:00:00.000Z", "")
	insightReadOnly(t, db)
	got, err := services.Sales.Retention(context.Background(), RetentionOptions{Now: retentionDay(t, "2026-03-01")})
	if err != nil || got.NeverVisitedCount != 1 || got.FirstVisitorCount != 0 || got.HistoryStart != "" {
		t.Fatalf("nonpositive deductions became visits: %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = services.Sales.Retention(ctx, RetentionOptions{}); err == nil {
		t.Fatal("cancelled context returned successful dashboard")
	}
}
