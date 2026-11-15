package service

import (
	"context"
	"testing"
)

func TestSalesCombinesInitialGenericAndPlanPricesWithinExactPeriod(t *testing.T) {
	db, s, _ := sharedFixture(t)
	ctx := context.Background()
	first, err := s.Member.Create(ctx, CreateMemberInput{Name: "Mixed customer", Phone: "01022223333", InitialCharge: 50000})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Member.Create(ctx, CreateMemberInput{Name: "Other customer", Phone: "01022224444"})
	if err != nil {
		t.Fatal(err)
	}
	setDate := func(id, date string) {
		t.Helper()
		if _, err := db.Exec("UPDATE transactions SET created_at=? WHERE id=?", date, id); err != nil {
			t.Fatal(err)
		}
	}
	var initialID string
	if err = db.QueryRow("SELECT id FROM transactions WHERE member_id=?", first.ID).Scan(&initialID); err != nil {
		t.Fatal(err)
	}
	since, until := "2026-02-01T15:00:00.000Z", "2026-02-02T15:00:00.000Z"
	setDate(initialID, since)
	generic, err := s.Charge.ChargeWithMemo(ctx, first.ID, nil, 70000, "generic sale", "admin")
	if err != nil {
		t.Fatal(err)
	}
	setDate(generic.Transaction.ID, "2026-02-02T00:00:00.000Z")
	plan := "plan-20-cash"
	sale, err := s.Charge.Charge(ctx, first.ID, &plan, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	setDate(sale.Transaction.ID, "2026-02-02T14:59:59.999Z")
	other, err := s.Charge.Charge(ctx, second.ID, nil, 300000, "admin")
	if err != nil {
		t.Fatal(err)
	}
	setDate(other.Transaction.ID, "2026-02-02T13:00:00.000Z")
	deduction, err := s.Charge.Deduct(ctx, first.ID, 5000, "service", "admin")
	if err != nil {
		t.Fatal(err)
	}
	setDate(deduction.Transaction.ID, "2026-02-02T14:00:00.000Z")
	before, err := s.Charge.Charge(ctx, first.ID, nil, 99000, "admin")
	if err != nil {
		t.Fatal(err)
	}
	setDate(before.Transaction.ID, "2026-02-01T14:59:59.999Z")
	// More than a recent-feed page after the selected interval must not hide its sales.
	for i := 0; i < 25; i++ {
		after, err := s.Charge.Charge(ctx, first.ID, nil, 88000, "admin")
		if err != nil {
			t.Fatal(err)
		}
		setDate(after.Transaction.ID, until)
	}
	summary, err := s.Sales.SalesSummary(ctx, since, until)
	if err != nil || summary.RealRevenue != 620000 || summary.CreditTotal != 660000 || summary.BonusCost != 40000 || summary.TxCount != 4 || summary.AvgTicket != 155000 || summary.ActiveMembers != 2 || summary.RepeatMembers != 1 || summary.PlanShareBP != 2500 {
		t.Fatalf("summary %+v %v", summary, err)
	}
	tiers, err := s.Sales.SalesByTier(ctx, since, until)
	if err != nil || len(tiers) != 2 {
		t.Fatalf("tiers %+v %v", tiers, err)
	}
	if tiers[0].TotalPrice != 200000 || tiers[0].TotalCredit != 240000 || tiers[0].TotalBonus != 40000 || tiers[0].Count != 1 || !tiers[1].IsGeneric || tiers[1].TotalPrice != 420000 || tiers[1].TotalCredit != 420000 || tiers[1].Count != 3 {
		t.Fatalf("tier amount disagreement %+v", tiers)
	}
	pay, err := s.Sales.SalesByPayMethod(ctx, since, until)
	if err != nil || len(pay) != 2 || pay[0].PayMethod != "cash" || pay[0].TotalPrice != 200000 || pay[0].Count != 1 || pay[1].PayMethod != "unknown" || pay[1].TotalPrice != 420000 || pay[1].Count != 3 {
		t.Fatalf("payment methods %+v %v", pay, err)
	}
	top, err := s.Sales.TopMembers(ctx, since, until, 5)
	if err != nil || len(top) != 2 || top[0].MemberID != first.ID || top[0].TotalPrice != 320000 || top[0].Count != 3 || top[1].TotalPrice != 300000 {
		t.Fatalf("top %+v %v", top, err)
	}
	daily, err := s.Sales.DailyRevenue(ctx, since, until)
	if err != nil || len(daily) != 1 || daily[0].Date != "2026-02-02" || daily[0].Revenue != 620000 {
		t.Fatalf("daily %+v %v", daily, err)
	}
	recent, err := s.Sales.RecentSales(ctx, since, until, 20)
	if err != nil || len(recent) != 4 || recent[0].TxID != sale.Transaction.ID || recent[0].Amount != 200000 || recent[0].CreditAmount != 240000 || recent[3].TxID != initialID || recent[3].Amount != 50000 {
		t.Fatalf("recent %+v %v", recent, err)
	}
}
