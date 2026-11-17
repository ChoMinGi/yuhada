package handler

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mingicho/yuhada/internal/sms"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
)

func ledgerPlanForm(t *testing.T, h *PageHandlers, planID string) url.Values {
	t.Helper()
	plan, err := h.Deps.Services.Queries.GetPlan(t.Context(), planID)
	if err != nil {
		t.Fatal(err)
	}
	quote := v2service.PlanQuoteFor(plan)
	return url.Values{
		"plan_id": {planID}, "expected_price": {strconv.FormatInt(quote.Price, 10)},
		"expected_credit": {strconv.FormatInt(quote.CreditAmount, 10)}, "expected_plan_version": {quote.Version},
	}
}

func TestPlanHTTPRequiresCurrentDisplayedQuoteAndNeverSendsSMSOnConflict(t *testing.T) {
	db, h, router, sender := handlerLedgerFixture(t)
	member, err := h.Deps.Services.Member.Create(t.Context(), v2service.CreateMemberInput{Name: "가상 확인 고객", Phone: "01012340000", InitialCharge: 10000})
	if err != nil {
		t.Fatal(err)
	}
	path := "/members/" + member.ID + "/charge"
	stale := ledgerPlanForm(t, h, "plan-20-card")
	if _, err = db.Exec("UPDATE membership_plans SET price=210000,credit_amount=230000 WHERE id='plan-20-card'"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TRIGGER reject_quote_wallet BEFORE UPDATE OF balance ON members BEGIN SELECT RAISE(ABORT,'unexpected wallet write'); END`); err != nil {
		t.Fatal(err)
	}
	for _, form := range []url.Values{
		{"plan_id": {"plan-20-card"}},
		{"plan_id": {"plan-20-card"}, "expected_price": {"210000"}, "expected_credit": {"230000"}},
		stale,
	} {
		w := ledgerPost(t, router, path, form)
		if w.Code != 409 || !strings.Contains(w.Body.String(), "충전 창을 닫고 다시 열어") || sender.checks.Load() != 0 {
			t.Fatalf("unconfirmed/stale quote response: %d %s SMS checks=%d", w.Code, w.Body.String(), sender.checks.Load())
		}
	}
	var txs, memberships, balance int
	if err = db.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&txs); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM memberships").Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT balance FROM members WHERE id=?", member.ID).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if txs != 1 || memberships != 0 || balance != 10000 {
		t.Fatalf("quote conflict persisted operation: tx=%d membership=%d balance=%d", txs, memberships, balance)
	}
	if _, err = db.Exec("DROP TRIGGER reject_quote_wallet"); err != nil {
		t.Fatal(err)
	}
	w := ledgerPost(t, router, path, ledgerPlanForm(t, h, "plan-20-card"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("fresh quote failed: %d %s", w.Code, w.Body.String())
	}
	expectSMS(t, sender, member.Phone, sms.ChargeMessage(member.Name, 230000, 240000))
}
