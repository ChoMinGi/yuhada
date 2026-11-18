package service

import (
	"context"
	"testing"
)

func TestAnalyticsExcludedMembersDoNotDisappearFromOperationalServices(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	all := New(db, nil)
	ctx := context.Background()
	owner, err := all.Member.Create(ctx, CreateMemberInput{Name: "Owner fixture", Phone: "01000000101", CardUUID: "owner-fixture-card"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := all.Member.Create(ctx, CreateMemberInput{Name: "Client A", Phone: "01000000102", CardUUID: "client-fixture-card"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := all.Member.Create(ctx, CreateMemberInput{Name: "Client B", Phone: "01000000103", CardUUID: "lost-fixture-card"})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		id      string
		balance int64
		active  bool
	}{{owner.ID, 9000000, true}, {a.ID, 10000, true}, {b.ID, 50000, false}} {
		if _, err = db.Exec("UPDATE members SET balance=?,is_active=? WHERE id=?", fixture.balance, fixture.active, fixture.id); err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range []struct {
		id, member, kind string
		amount           int64
	}{{"own-in", owner.ID, "charge", 9000000}, {"own-out", owner.ID, "deduct", 8000000}, {"a-in", a.ID, "charge", 100000}, {"a-out", a.ID, "deduct", 10000}, {"b-in", b.ID, "charge", 50000}, {"b-out", b.ID, "deduct", 7000}, {"b-refund", b.ID, "refund", 1000}} {
		if _, err = db.Exec(`INSERT INTO transactions(id,member_id,type,amount,balance_after,created_at) VALUES(?,?,?,?,0,'2026-01-01T00:00:00.000Z')`, fixture.id, fixture.member, fixture.kind, fixture.amount); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{" " + owner.ID + " ", owner.ID, ""}
	scoped := New(db, nil, ids...)
	ids[0] = a.ID
	if _, err = db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	got, err := scoped.Stats.Dashboard(ctx, PeriodAll)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChargeTotal != 150000 || got.DeductTotal != 17000 || got.TotalLiability != 10000 || got.MemberCount != 1 || got.CardsIssued != 2 || got.CardsLost != 1 {
		t.Fatalf("analytics included owner or dropped inactive history: %+v", got)
	}
	baseline, err := all.Stats.Dashboard(ctx, PeriodAll)
	if err != nil || baseline.ChargeTotal != 9150000 || baseline.MemberCount != 2 || baseline.TotalLiability != 9010000 || baseline.CardsIssued != 3 {
		t.Fatalf("empty exclusions changed old behavior %+v %v", baseline, err)
	}
	other, err := New(db, nil, owner.ID, b.ID).Stats.Dashboard(ctx, PeriodAll)
	if err != nil || other.ChargeTotal != 100000 || other.CardsIssued != 1 || other.CardsLost != 0 {
		t.Fatalf("multiple excluded IDs %+v %v", other, err)
	}
	member, err := scoped.Member.Get(ctx, owner.ID)
	if err != nil || member.Balance != 9000000 || member.Name != "Owner fixture" {
		t.Fatal("owner operational member changed", err)
	}
	history, err := scoped.Tx.ListByMember(ctx, owner.ID)
	if err != nil || len(history) != 2 {
		t.Fatal("owner operational history disappeared", err)
	}
	card, err := scoped.Member.GetByCardUUID(ctx, "owner-fixture-card")
	if err != nil || card.ID != owner.ID {
		t.Fatal("owner card became inaccessible", err)
	}
	var members, transactions int
	if err = db.QueryRow("SELECT COUNT(*) FROM members").Scan(&members); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if members != 3 || transactions != 7 {
		t.Fatal("analytics changed source rows")
	}
}
