package service

import (
	"context"
	"testing"
)

func TestAnalyticsExclusionPreservesLedgerAndRevenue(t *testing.T) {
	db, original, _ := sharedFixture(t)
	ctx := context.Background()
	keep := insightMember(t, original, 8101, 345000, true)
	excluded := insightMember(t, original, 8102, 678000, true)
	at := "2026-02-01T00:00:00.000Z"
	insightTx(t, db, "legacy-unknown", keep, "charge", 100000, at, "")
	insightTx(t, db, "package", keep, "charge", 240000, at, "")
	insightTx(t, db, "excluded-charge", excluded, "charge", 900000, at, "")
	insightTx(t, db, "kept-visit", keep, "deduct", 10000, at, "")
	insightTx(t, db, "excluded-visit", excluded, "deduct", 20000, at, "")
	if _, err := db.Exec(`INSERT INTO memberships(id,member_id,plan_snapshot,charge_tx_id) VALUES('package-membership',?,'{"price":200000,"credit_amount":240000}','package')`, keep); err != nil {
		t.Fatal(err)
	}
	scoped := New(db, original.Enc, excluded)
	insightReadOnly(t, db)
	since, until := "2026-01-01", "2026-03-01"
	base, err := original.Sales.SalesSummary(ctx, since, until)
	if err != nil {
		t.Fatal(err)
	}
	got, err := scoped.Sales.SalesSummary(ctx, since, until)
	if err != nil {
		t.Fatal(err)
	}
	if base.RealRevenue != 1200000 || got.RealRevenue != 300000 || got.CreditTotal != 340000 || got.BonusCost != 40000 || got.TxCount != 2 {
		t.Fatalf("revenue: base=%+v scoped=%+v", base, got)
	}
	insights, err := scoped.Sales.Insights(ctx, since, until, insightTime(t, "2026-02-03T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if insights.FirstSalesAmount+insights.RepeatSalesAmount != 300000 || insights.UsageTotal != 10000 || insights.VisitorCount != 1 || insights.CurrentMemberCount != 1 || insights.CurrentBalance != 345000 {
		t.Fatalf("insights=%+v", insights)
	}
	activity, err := scoped.Sales.RecentActivity(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range activity {
		if row.MemberID == excluded {
			t.Fatal("excluded member in analytics feed")
		}
	}
	member, err := scoped.Member.Get(ctx, excluded)
	if err != nil || member.Balance != 678000 {
		t.Fatalf("operational member/ balance changed: %+v %v", member, err)
	}
	var count int
	var amount int64
	if err := db.QueryRow(`SELECT COUNT(*),SUM(amount) FROM transactions WHERE member_id=?`, excluded).Scan(&count, &amount); err != nil {
		t.Fatal(err)
	}
	if count != 2 || amount != 920000 {
		t.Fatalf("ledger changed count=%d amount=%d", count, amount)
	}
}
