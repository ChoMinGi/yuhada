package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

func insightMember(t *testing.T, s *Services, number int, balance int64, active bool) string {
	t.Helper()
	m, err := s.Member.Create(context.Background(), CreateMemberInput{Name: fmt.Sprintf("insight fixture %d", number), Phone: fmt.Sprintf("010%08d", number)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE members SET balance=?,is_active=? WHERE id=?", balance, active, m.ID); err != nil {
		t.Fatal(err)
	}
	return m.ID
}
func insightTx(t *testing.T, db *sql.DB, id, memberID, kind string, amount int64, at, memo string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO transactions(id,member_id,type,amount,balance_after,created_at,memo) VALUES(?,?,?,?,0,?,?)`, id, memberID, kind, amount, at, memo); err != nil {
		t.Fatal(err)
	}
}
func insightReadOnly(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
}
func insightTime(t *testing.T, value string) time.Time {
	t.Helper()
	got, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestInsightsLifetimeFirstSaleAndKSTUsageBoundaries(t *testing.T) {
	db, s, _ := sharedFixture(t)
	ctx := context.Background()
	a := insightMember(t, s, 1, 0, true)
	b := insightMember(t, s, 2, 0, true)
	c := insightMember(t, s, 3, 0, true)
	since, until := "2026-01-31T15:00:00.000Z", "2026-02-02T15:00:00.000Z"
	insightTx(t, db, "a-old", a, "charge", 100000, "2026-01-01T00:00:00.000Z", "not an initial memo")
	insightTx(t, db, "a-repeat", a, "charge", 70000, "2026-02-01T00:00:00.000Z", "최초 충전")
	insightTx(t, db, "b-first", b, "charge", 240000, since, "")
	if _, err := db.Exec(`INSERT INTO memberships(id,member_id,plan_snapshot,charge_tx_id) VALUES('snapshot-sale',?,'{"price":200000,"credit_amount":240000}','b-first')`, b); err != nil {
		t.Fatal(err)
	}
	insightTx(t, db, "b-repeat", b, "charge", 30000, since, "최초 충전")
	// Insertion order is deliberately the reverse of the deterministic ID tie.
	insightTx(t, db, "c-z", c, "charge", 20000, "2026-02-01T15:00:00.000Z", "")
	insightTx(t, db, "c-a", c, "charge", 10000, "2026-02-01T15:00:00.000Z", "")
	insightTx(t, db, "usage-start", b, "deduct", 5000, since, "")
	insightTx(t, db, "usage-before-midnight", a, "deduct", 7000, "2026-02-01T14:59:59.999Z", "")
	insightTx(t, db, "usage-midnight", c, "deduct", 9000, "2026-02-01T15:00:00.000Z", "")
	insightTx(t, db, "usage-repeat-visitor", b, "deduct", 1000, "2026-02-02T00:00:00.000Z", "")
	insightTx(t, db, "refund-is-not-usage", a, "refund", 50000, "2026-02-01T01:00:00.000Z", "")
	insightTx(t, db, "usage-end-excluded", a, "deduct", 100000, until, "")
	insightTx(t, db, "usage-before-start", a, "deduct", 11000, "2026-01-31T14:59:59.999Z", "")
	insightReadOnly(t, db)
	now := insightTime(t, "2026-02-03T12:00:00.000Z")
	got, err := s.Sales.Insights(ctx, since, until, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsageTotal != 22000 || got.VisitCount != 4 || got.VisitorCount != 3 {
		t.Fatalf("usage total/counts %+v", got)
	}
	if got.FirstSalesCount != 2 || got.FirstSalesAmount != 210000 || got.RepeatSalesCount != 3 || got.RepeatSalesAmount != 120000 {
		t.Fatalf("first/repeat sales %+v", got)
	}
	summary, err := s.Sales.SalesSummary(ctx, since, until)
	if err != nil || summary.RealRevenue != got.FirstSalesAmount+got.RepeatSalesAmount || summary.TxCount != got.FirstSalesCount+got.RepeatSalesCount {
		t.Fatalf("sales decomposition mismatch %+v %v", summary, err)
	}
	if len(got.DailyUsage) != 2 || got.DailyUsage[0] != (DailyBucket{Date: "2026-02-01", Revenue: 12000}) || got.DailyUsage[1] != (DailyBucket{Date: "2026-02-02", Revenue: 10000}) {
		t.Fatalf("KST daily usage %+v", got.DailyUsage)
	}
	empty, err := s.Sales.Insights(ctx, "1990", "1991", now)
	if err != nil {
		t.Fatal(err)
	}
	if empty.UsageTotal != 0 || empty.VisitCount != 0 || empty.VisitorCount != 0 || empty.FirstSalesCount != 0 || empty.RepeatSalesCount != 0 || len(empty.DailyUsage) != 0 {
		t.Fatalf("empty period activity %+v", empty)
	}
	if empty.CurrentMemberCount != got.CurrentMemberCount || empty.CurrentBalance != got.CurrentBalance || empty.LowBalanceCount != got.LowBalanceCount {
		t.Fatal("period changed the current member snapshot")
	}
}

func TestInsightsBalanceBucketsPreserveAllMembersAndBoundaryAmounts(t *testing.T) {
	db, s, _ := sharedFixture(t)
	values := []int64{0, 1, 50000, 50001, 100000, 100001, 300000, 300001}
	for i, balance := range values {
		insightMember(t, s, i+10, balance, i%2 == 0)
	}
	insightReadOnly(t, db)
	got, err := s.Sales.Insights(context.Background(), "1970", "", insightTime(t, "2026-02-03T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentMemberCount != 8 || got.CurrentBalance != 900004 || len(got.BalanceBuckets) != 5 {
		t.Fatalf("whole member snapshot %+v", got)
	}
	wantCounts := []int{1, 2, 2, 2, 1}
	wantBalances := []int64{0, 50001, 150001, 400001, 300001}
	var count int
	var total int64
	for i, bucket := range got.BalanceBuckets {
		if bucket.MemberCount != wantCounts[i] || bucket.Balance != wantBalances[i] {
			t.Fatalf("boundary bucket %d %+v", i, bucket)
		}
		count += bucket.MemberCount
		total += bucket.Balance
	}
	if count != got.CurrentMemberCount || total != got.CurrentBalance || got.LowBalanceCount != 0 {
		t.Fatal("buckets lost inactive members, balance, or included never-visited candidates")
	}
}

func TestInsightsLowBalanceCandidatesUseAllRecentDeductsAndSeparateCount(t *testing.T) {
	db, s, _ := sharedFixture(t)
	now := insightTime(t, "2026-03-15T03:00:00.000Z")
	cutoff := now.AddDate(0, 0, -60)
	iso := func(at time.Time) string { return at.Format("2006-01-02T15:04:05.000Z") }
	eligible := map[string]int64{}
	for i, balance := range []int64{0, 10000, 20000, 30000, 40000, 50000, 5000} {
		id := insightMember(t, s, i+30, balance, true)
		eligible[id] = balance
		at := now.Add(-time.Duration(i+1) * 24 * time.Hour)
		if balance == 50000 {
			at = cutoff
		}
		insightTx(t, db, fmt.Sprintf("candidate-%d", i), id, "deduct", 1000, iso(at), "")
	}
	// A recent refund/charge does not make a visitor; inactive, high-balance,
	// future-only and one millisecond too-old deductions are also excluded.
	for i, fixture := range []struct {
		balance int64
		active  bool
		kind    string
		at      time.Time
	}{{50001, true, "deduct", now}, {100, false, "deduct", now}, {100, true, "deduct", cutoff.Add(-time.Millisecond)}, {100, true, "refund", now}, {100, true, "charge", now}, {100, true, "deduct", now.Add(time.Millisecond)}} {
		id := insightMember(t, s, i+50, fixture.balance, fixture.active)
		insightTx(t, db, fmt.Sprintf("excluded-%d", i), id, fixture.kind, 1000, iso(fixture.at), "")
	}
	var lowestID string
	for id, balance := range eligible {
		if balance == 0 {
			lowestID = id
		}
	}
	insightTx(t, db, "candidate-last-visit", lowestID, "deduct", 1000, iso(now), "")
	insightTx(t, db, "candidate-future-visit", lowestID, "deduct", 1000, iso(now.Add(time.Millisecond)), "")
	insightReadOnly(t, db)
	got, err := s.Sales.Insights(context.Background(), "1990", "1991", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.LowBalanceCount != 7 || len(got.LowBalanceMembers) != 5 {
		t.Fatalf("candidate count was limited or period-filtered: count=%d listed=%d", got.LowBalanceCount, len(got.LowBalanceMembers))
	}
	for i, member := range got.LowBalanceMembers {
		if _, ok := eligible[member.MemberID]; !ok {
			t.Fatal("ineligible candidate listed")
		}
		if !strings.HasPrefix(member.Name, "insight fixture ") {
			t.Fatal("candidate name was not decrypted")
		}
		if member.LastVisitAt == "" {
			t.Fatal("candidate missing last visit")
		}
		if i > 0 && got.LowBalanceMembers[i-1].Balance > member.Balance {
			t.Fatal("candidate order is not balance ascending")
		}
	}
	if got.LowBalanceMembers[0].LastVisitAt != iso(now) {
		t.Fatal("last visit did not use the latest nonfuture deduction")
	}
	if got.LowBalanceMembers[0].Balance != 0 || got.LowBalanceMembers[1].Balance != 5000 || got.LowBalanceMembers[4].Balance != 30000 {
		t.Fatal("candidate top five did not use lowest current balances")
	}
}

func TestInsightsUsageHasNoHistoryDisplayLimit(t *testing.T) {
	db, s, _ := sharedFixture(t)
	id := insightMember(t, s, 80, 0, true)
	for i := 0; i < 605; i++ {
		insightTx(t, db, fmt.Sprintf("usage-%04d", i), id, "deduct", 1000, "2026-03-01T00:00:00.000Z", "")
	}
	insightTx(t, db, "refund-extra", id, "refund", 999999, "2026-03-01T00:00:00.000Z", "")
	insightReadOnly(t, db)
	got, err := s.Sales.Insights(context.Background(), "1970", "", insightTime(t, "2026-03-02T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if got.UsageTotal != 605000 || got.VisitCount != 605 || got.VisitorCount != 1 || len(got.DailyUsage) != 1 || got.DailyUsage[0].Revenue != 605000 {
		t.Fatalf("truncated/refund-inflated usage %+v", got)
	}
}
