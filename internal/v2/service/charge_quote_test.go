package service

import (
	"context"
	"errors"
	"testing"
)

func TestPlanQuoteRejectsChangedOfferBeforeAnyWalletWrite(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"price", "UPDATE membership_plans SET price=210000 WHERE id='plan-20-card'"},
		{"credit", "UPDATE membership_plans SET credit_amount=240000 WHERE id='plan-20-card'"},
		{"label", "UPDATE membership_plans SET tier_label='변경된 회원권' WHERE id='plan-20-card'"},
		{"payment", "UPDATE membership_plans SET payment_method='cash' WHERE id='plan-20-card'"},
		{"perk", "UPDATE membership_plans SET perk_label='변경된 증정품' WHERE id='plan-20-card'"},
	} {
		t.Run(change.name, func(t *testing.T) {
			db, s, _ := sharedFixture(t)
			ctx := context.Background()
			member := fixtureMember(t, s)
			planID := "plan-20-card"
			plan, err := s.Queries.GetPlan(ctx, planID)
			if err != nil {
				t.Fatal(err)
			}
			quote := PlanQuoteFor(plan)
			// No updated_at change: hashing offer values must also catch edits
			// occurring inside the same database timestamp resolution.
			if _, err = db.Exec(change.sql); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`CREATE TRIGGER reject_quote_balance BEFORE UPDATE OF balance ON members BEGIN SELECT RAISE(ABORT,'unexpected wallet write'); END;
				CREATE TRIGGER reject_quote_tx BEFORE INSERT ON transactions BEGIN SELECT RAISE(ABORT,'unexpected transaction write'); END;`); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Charge.ChargeWithExpectedPlan(ctx, member, &planID, 0, "", "audit", &quote); !errors.Is(err, ErrPlanQuoteChanged) {
				t.Fatalf("stale quote did not fail before writes: %v", err)
			}
			assertBalance(t, s, member, 0)
			if tableCount(t, db, "transactions") != 0 || tableCount(t, db, "memberships") != 0 {
				t.Fatal("stale offer changed sale/ledger rows")
			}
			if _, err = db.Exec("DROP TRIGGER reject_quote_balance; DROP TRIGGER reject_quote_tx"); err != nil {
				t.Fatal(err)
			}
			plan, err = s.Queries.GetPlan(ctx, planID)
			if err != nil {
				t.Fatal(err)
			}
			fresh := PlanQuoteFor(plan)
			result, err := s.Charge.ChargeWithExpectedPlan(ctx, member, &planID, 0, "", "audit", &fresh)
			if err != nil || result.NewBalance != plan.CreditAmount || result.MembershipID == "" {
				t.Fatalf("fresh confirmed offer rejected: %+v %v", result, err)
			}
		})
	}
}

func TestPlanQuoteCannotBeReusedForDifferentOrDeletedPlan(t *testing.T) {
	db, s, _ := sharedFixture(t)
	ctx := context.Background()
	member := fixtureMember(t, s)
	card, err := s.Queries.GetPlan(ctx, "plan-20-card")
	if err != nil {
		t.Fatal(err)
	}
	quote := PlanQuoteFor(card)
	cashID := "plan-20-cash"
	if _, err = s.Charge.ChargeWithExpectedPlan(ctx, member, &cashID, 0, "", "audit", &quote); !errors.Is(err, ErrPlanQuoteChanged) {
		t.Fatalf("quote reused for different plan: %v", err)
	}
	if _, err = db.Exec("DELETE FROM membership_plans WHERE id=?", card.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Charge.ChargeWithExpectedPlan(ctx, member, &card.ID, 0, "", "audit", &quote); !errors.Is(err, ErrPlanQuoteChanged) {
		t.Fatalf("deleted quote did not request refreshed offer: %v", err)
	}
	assertBalance(t, s, member, 0)
	if tableCount(t, db, "transactions") != 0 || tableCount(t, db, "memberships") != 0 {
		t.Fatal("invalid quote wrote ledger data")
	}
}
