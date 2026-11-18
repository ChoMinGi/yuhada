package service

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"
)

func TestAnalyticsExclusionReconcilesSalesInsightsAndOperationalLedger(t *testing.T) {
	db, all, _ := sharedFixture(t)
	ctx := context.Background()
	owner := insightMember(t, all, 700, 0, true)
	a := insightMember(t, all, 701, 10000, true)
	b := insightMember(t, all, 702, 40000, false)
	for _, r := range []struct {
		id, member, kind, at string
		amount               int64
	}{
		{"owner-first", owner, "charge", "2026-03-14T10:00:00.000Z", 9000000},
		{"owner-repeat", owner, "charge", "2026-03-14T11:00:00.000Z", 1000000},
		{"owner-usage", owner, "deduct", "2026-03-14T12:00:00.000Z", 999999},
		{"a-before-window", a, "charge", "2026-02-01T00:00:00.000Z", 10000},
		{"a-plan", a, "charge", "2026-03-01T00:00:00.000Z", 240000},
		{"a-repeat", a, "charge", "2026-03-02T00:00:00.000Z", 30000},
		{"b-first", b, "charge", "2026-03-01T00:00:00.000Z", 50000},
		{"a-usage-1", a, "deduct", "2026-03-05T00:00:00.000Z", 5000},
		{"a-usage-2", a, "deduct", "2026-03-06T00:00:00.000Z", 7000},
		{"b-usage", b, "deduct", "2026-03-02T00:00:00.000Z", 8000},
		{"b-refund", b, "refund", "2026-03-03T00:00:00.000Z", 2000},
	} {
		insightTx(t, db, r.id, r.member, r.kind, r.amount, r.at, "")
	}
	for _, r := range []struct{ id, member, charge, snapshot string }{
		{"owner-membership", owner, "owner-first", `{"tier_label":"Owner fixture","price":9000000,"credit_amount":9000000,"payment_method":"card"}`},
		{"a-membership", a, "a-plan", `{"tier_label":"Client fixture","tier_amount":200000,"price":200000,"credit_amount":240000,"payment_method":"cash"}`},
	} {
		if _, err := db.Exec(`INSERT INTO memberships(id,member_id,charge_tx_id,plan_snapshot,purchased_at) VALUES(?,?,?,?,'2025-11-05T00:00:00.000Z')`, r.id, r.member, r.charge, r.snapshot); err != nil {
			t.Fatal(err)
		}
	}
	scoped := New(db, all.Enc, " "+owner+" ", owner, "")
	if len(scoped.AnalyticsExcludedMemberIDs) != 1 {
		t.Fatal("constructor failed to normalize exclusions")
	}
	before := tableCount(t, db, "transactions")
	insightReadOnly(t, db)
	now := insightTime(t, "2026-03-15T03:00:00.000Z")
	since, until := "2026-03-01T00:00:00.000Z", "2026-04-01T00:00:00.000Z"
	summary, err := scoped.Sales.SalesSummary(ctx, since, until)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RealRevenue != 280000 || summary.CreditTotal != 320000 || summary.BonusCost != 40000 || summary.TxCount != 3 || summary.ActiveMembers != 2 || summary.RepeatMembers != 1 {
		t.Fatalf("sales population/price/bonus wrong %+v", summary)
	}
	insights, err := scoped.Sales.Insights(ctx, since, until, now)
	if err != nil {
		t.Fatal(err)
	}
	if insights.UsageTotal != 20000 || insights.VisitorCount != 2 || insights.VisitCount != 3 || insights.FirstSalesCount != 1 || insights.FirstSalesAmount != 50000 || insights.RepeatSalesCount != 2 || insights.RepeatSalesAmount != 230000 {
		t.Fatalf("period insights wrong %+v", insights)
	}
	if insights.CurrentMemberCount != 2 || insights.CurrentBalance != 50000 || insights.LowBalanceCount != 1 || len(insights.LowBalanceMembers) != 1 || insights.LowBalanceMembers[0].MemberID != a {
		t.Fatalf("current stock/candidates wrong %+v", insights)
	}
	var bucketCount int
	var bucketBalance int64
	for _, bucket := range insights.BalanceBuckets {
		bucketCount += bucket.MemberCount
		bucketBalance += bucket.Balance
	}
	if bucketCount != 2 || bucketBalance != 50000 || insights.BalanceBuckets[0].MemberCount != 0 {
		t.Fatal("balance buckets retained excluded owner")
	}
	daily, err := scoped.Sales.DailyRevenue(ctx, since, until)
	if err != nil || len(daily) != 2 || daily[0].Revenue != 250000 || daily[1].Revenue != 30000 {
		t.Fatalf("daily price totals %+v %v", daily, err)
	}
	var usage int64
	for _, day := range insights.DailyUsage {
		usage += day.Revenue
	}
	if usage != 20000 {
		t.Fatal("daily usage scope disagrees")
	}
	tiers, err := scoped.Sales.SalesByTier(ctx, since, until)
	if err != nil {
		t.Fatal(err)
	}
	var tierSum int64
	var tierCount int
	for _, r := range tiers {
		tierSum += r.TotalPrice
		tierCount += r.Count
	}
	pay, err := scoped.Sales.SalesByPayMethod(ctx, since, until)
	if err != nil {
		t.Fatal(err)
	}
	var paySum int64
	var payCount int
	for _, r := range pay {
		paySum += r.TotalPrice
		payCount += r.Count
	}
	if tierSum != 280000 || paySum != 280000 || tierCount != 3 || payCount != 3 {
		t.Fatal("tier/pay breakdown changed denominator")
	}
	top, err := scoped.Sales.TopMembers(ctx, since, until, 1)
	if err != nil || len(top) != 1 || top[0].MemberID != a || top[0].TotalPrice != 230000 {
		t.Fatalf("top scope after limit %+v %v", top, err)
	}
	recent, err := scoped.Sales.RecentSales(ctx, since, until, 1)
	if err != nil || len(recent) != 1 || recent[0].TxID != "a-repeat" {
		t.Fatalf("recent scope after limit %+v %v", recent, err)
	}
	activity, err := scoped.Sales.RecentActivity(ctx, 1)
	if err != nil || len(activity) != 1 || activity[0].TxID != "a-usage-2" {
		t.Fatalf("activity scope after limit %+v %v", activity, err)
	}
	ledger, err := scoped.Sales.LedgerTotals(ctx, since, until)
	if err != nil || ledger.Charge != 320000 || ledger.Deduct != 22000 {
		t.Fatalf("ledger totals lost refund semantics %+v %v", ledger, err)
	}
	expiring, err := scoped.Sales.ExpiringMemberships(ctx, "2026-11-01T00:00:00.000Z", "2026-12-01T00:00:00.000Z")
	if err != nil || len(expiring) != 1 || expiring[0].MemberID != a {
		t.Fatalf("old expiry helper retained owner %+v %v", expiring, err)
	}
	for _, kind := range []string{"top", "recent", "recharge"} {
		got, err := scoped.Sales.ListDetails(ctx, kind, since, until, "insight fixture 700", 1, now)
		if err != nil || got.Total != 0 || len(got.Rows) != 0 {
			t.Fatalf("excluded owner could reenter %s through search %+v %v", kind, got, err)
		}
	}
	member, err := scoped.Member.Get(ctx, owner)
	if err != nil || member.ID != owner || member.Balance != 0 {
		t.Fatal("operational owner changed", err)
	}
	history, err := scoped.Queries.ListMemberHistoryWithPlan(ctx, owner)
	if err != nil || len(history) != 3 {
		t.Fatal("owner ledger history lost", err)
	}
	baseline, err := all.Sales.SalesSummary(ctx, since, until)
	if err != nil || baseline.RealRevenue-summary.RealRevenue != 10000000 {
		t.Fatalf("empty exclusions did not retain original scope %+v %v", baseline, err)
	}
	if tableCount(t, db, "members") != 3 || tableCount(t, db, "transactions") != before || tableCount(t, db, "memberships") != 2 {
		t.Fatal("analytics modified operational rows")
	}
}

func TestAnalyticsExclusionPrecedesRetentionHistoryCohortsAndQueues(t *testing.T) {
	db, all, _ := sharedFixture(t)
	owner := insightMember(t, all, 710, 100000, true)
	a := insightMember(t, all, 711, 10000, true)
	b := insightMember(t, all, 712, 10000, false)
	never := insightMember(t, all, 713, 10000, true)
	dormant := insightMember(t, all, 714, 10000, true)
	for i, r := range []struct{ id, day string }{{owner, "2020-01-01"}, {owner, "2026-01-02"}, {owner, "2026-02-02"}, {owner, "2026-03-14"}, {a, "2025-12-31"}, {a, "2026-01-10"}, {a, "2026-02-10"}, {a, "2026-03-10"}, {b, "2026-01-20"}, {b, "2026-03-01"}, {dormant, "2025-12-01"}} {
		date := retentionDay(t, r.day).Add(12 * time.Hour)
		insightTx(t, db, fmt.Sprintf("scope-visit-%02d", i), r.id, "deduct", 1000, retentionISO(date), "")
	}
	_ = never
	scoped := New(db, all.Enc, owner)
	insightReadOnly(t, db)
	now := insightTime(t, "2026-03-15T03:00:00.000Z")
	got, err := scoped.Sales.Retention(context.Background(), RetentionOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 3 || got.Months[0].Month != "2026-01" || got.HistoryStart != "2025-12-01" || got.LatestVisit != "2026-03-10" {
		t.Fatalf("owner history or default window leaked %+v", got)
	}
	if got.GapCount != 4 || math.Abs(got.MeanGapDays-27.25) > .000001 || got.FirstVisitorCount != 1 || got.ReturningCustomerCount != 2 {
		t.Fatalf("owner affected gaps/first visits %+v", got)
	}
	if got.Months[0].Visitors != 2 || got.Months[0].Returning != 1 || got.Months[1].Visitors != 1 || got.Months[1].Returning != 1 || got.Months[2].Visitors != 2 || got.Months[2].Returning != 2 {
		t.Fatalf("month ratios lost inactive customer or retained owner %+v", got.Months)
	}
	if got.NeverVisitedCount != 1 || got.DormantCount != 1 || len(got.DormantMembers) != 1 || got.DormantMembers[0].MemberID != dormant {
		t.Fatalf("filtered visits left owner in never-visited/current queue %+v", got)
	}
	if len(got.Cohorts) != 1 || got.Cohorts[0].Month != "2026-01" || got.Cohorts[0].Size != 1 || got.Cohorts[0].Cells[0] != (RetentionCell{Offset: 1, Returning: 0, Status: "complete"}) || got.Cohorts[0].Cells[1] != (RetentionCell{Offset: 2, Returning: 1, Status: "observing"}) {
		t.Fatalf("cohort denominator/maturity changed %+v", got.Cohorts)
	}
	legacyDormant, err := scoped.Sales.DormantMembers(context.Background(), "2026-03-15T23:59:59.999Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range legacyDormant {
		if m.MemberID == owner {
			t.Fatal("unused dormant helper retained owner")
		}
	}
	baselineDormant, err := all.Sales.DormantMembers(context.Background(), "2026-03-15T23:59:59.999Z")
	if err != nil {
		t.Fatal(err)
	}
	ownerWasEligible := false
	for _, m := range baselineDormant {
		ownerWasEligible = ownerWasEligible || m.MemberID == owner
	}
	if !ownerWasEligible {
		t.Fatal("dormant exclusion fixture did not qualify without the policy")
	}
	baseline, err := all.Sales.Retention(context.Background(), RetentionOptions{Now: now})
	if err != nil || baseline.HistoryStart != "2020-01-01" || baseline.LatestVisit != "2026-03-14" || baseline.GapCount <= got.GapCount {
		t.Fatalf("unconfigured constructor lost owner baseline %+v %v", baseline, err)
	}
	if tableCount(t, db, "members") != 5 || tableCount(t, db, "transactions") != 11 {
		t.Fatal("retention changed source history")
	}
}

func TestAnalyticsExclusionRunsBeforeCandidateLimitAndListPages(t *testing.T) {
	db, all, _ := sharedFixture(t)
	owner := insightMember(t, all, 720, 0, true)
	for i := 0; i < 25; i++ {
		insightTx(t, db, fmt.Sprintf("latest-owner-sale-%02d", i), owner, "charge", 999999, "2026-03-14T00:00:00.000Z", "")
		insightTx(t, db, fmt.Sprintf("latest-owner-use-%02d", i), owner, "deduct", 999999, "2026-03-14T01:00:00.000Z", "")
	}
	for i := 0; i < 21; i++ {
		id := insightMember(t, all, 721+i, int64(1000+i), true)
		insightTx(t, db, fmt.Sprintf("real-sale-%02d", i), id, "charge", 1000, "2026-03-01T00:00:00.000Z", "")
		insightTx(t, db, fmt.Sprintf("real-use-%02d", i), id, "deduct", 100, "2026-03-02T00:00:00.000Z", "")
	}
	scoped := New(db, all.Enc, owner)
	insightReadOnly(t, db)
	now := insightTime(t, "2026-03-15T03:00:00.000Z")
	insights, err := scoped.Sales.Insights(context.Background(), "1970", "", now)
	if err != nil || insights.LowBalanceCount != 21 || len(insights.LowBalanceMembers) != 5 {
		t.Fatalf("candidate filtered after top5/count %+v %v", insights, err)
	}
	for _, m := range insights.LowBalanceMembers {
		if m.MemberID == owner {
			t.Fatal("owner candidate leaked")
		}
	}
	for _, kind := range []string{"recent", "top", "recharge"} {
		seen := map[string]bool{}
		for page, want := range []int{20, 1} {
			got, err := scoped.Sales.ListDetails(context.Background(), kind, "1970", "", "", page+1, now)
			if err != nil || got.Total != 21 || got.Pages != 2 || len(got.Rows) != want {
				t.Fatalf("%s page%d counted owner or lost page %+v %v", kind, page+1, got, err)
			}
			for _, r := range got.Rows {
				if r.MemberID == owner || seen[r.ID] {
					t.Fatalf("%s duplicate/excluded row %+v", kind, r)
				}
				seen[r.ID] = true
			}
		}
		if len(seen) != 21 {
			t.Fatal("filtered page lost real records")
		}
	}
	activity, err := scoped.Sales.RecentActivity(context.Background(), 8)
	if err != nil || len(activity) != 8 {
		t.Fatalf("owner exhausted activity limit %+v %v", activity, err)
	}
	for _, r := range activity {
		if r.MemberID == owner {
			t.Fatal("owner in limited activity feed")
		}
	}
	if tableCount(t, db, "members") != 22 || tableCount(t, db, "transactions") != 92 {
		t.Fatal("paged analytics deleted source records")
	}
}

func TestAnalyticsOnlyExcludedOwnerHasEmptyCohortsAndZeroDenominators(t *testing.T) {
	db, all, _ := sharedFixture(t)
	owner := insightMember(t, all, 750, 5000000, true)
	insightTx(t, db, "only-owner-charge", owner, "charge", 5000000, "2026-01-20T00:00:00.000Z", "")
	insightTx(t, db, "only-owner-first", owner, "deduct", 1000, "2026-01-20T01:00:00.000Z", "")
	insightTx(t, db, "only-owner-repeat", owner, "deduct", 1000, "2026-02-20T01:00:00.000Z", "")
	scoped := New(db, all.Enc, owner)
	insightReadOnly(t, db)
	now := insightTime(t, "2026-03-15T03:00:00.000Z")
	got, err := scoped.Sales.Retention(context.Background(), RetentionOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 3 || got.FirstVisitorCount != 0 || got.ReturningCustomerCount != 0 || got.GapCount != 0 || got.MeanGapDays != 0 || got.NeverVisitedCount != 0 || got.DormantCount != 0 || len(got.Cohorts) != 0 || got.HistoryStart != "" || got.LatestVisit != "" {
		t.Fatalf("excluded-only history invented analytics population: %+v", got)
	}
	for _, month := range got.Months {
		if month.Visitors != 0 || month.Returning != 0 || month.VisitDays != 0 || month.GapCount != 0 {
			t.Fatalf("nonempty month %+v", month)
		}
	}
	insights, err := scoped.Sales.Insights(context.Background(), "1970", "", now)
	if err != nil || insights.CurrentMemberCount != 0 || insights.CurrentBalance != 0 || insights.UsageTotal != 0 || insights.FirstSalesCount != 0 || insights.RepeatSalesCount != 0 || insights.LowBalanceCount != 0 {
		t.Fatalf("excluded-only insights %+v %v", insights, err)
	}
	summary, err := scoped.Sales.SalesSummary(context.Background(), "1970", "")
	if err != nil || summary.TxCount != 0 || summary.RealRevenue != 0 || summary.CreditTotal != 0 {
		t.Fatalf("excluded-only sale %+v %v", summary, err)
	}
	baseline, err := all.Sales.Retention(context.Background(), RetentionOptions{Now: now})
	if err != nil || baseline.FirstVisitorCount != 1 || len(baseline.Cohorts) != 1 || baseline.Cohorts[0].Month != "2026-01" || baseline.Cohorts[0].Size != 1 {
		t.Fatalf("owner did not qualify for in-window cohort before exclusion %+v %v", baseline, err)
	}
	if _, err = scoped.Member.Get(context.Background(), owner); err != nil || tableCount(t, db, "transactions") != 3 {
		t.Fatal("zero analytics erased operational owner", err)
	}
}
