package page

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDeductPresetAllowsStandardAndCustomWonAmounts(t *testing.T) {
	var rendered bytes.Buffer
	if err := SettingsDeductAmounts(SettingsDeductAmountsProps{Rows: []DeductAmountRow{{ID: "fixture", Amount: 20000, IsActive: true}}}).Render(context.Background(), &rendered); err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	if strings.Count(html, `min="1" step="1"`) != 2 || strings.Contains(html, `step="1000"`) {
		t.Fatal("new and existing preset forms must accept any positive whole-won amount, including20000 and23000")
	}
	if !strings.Contains(html, `window.yhSavePresetOrder('/v2/admin/settings/deduct-amounts/reorder', ids)`) {
		t.Fatal("drag order must use a response-checked save")
	}
}

func TestInitialChargeExplainsPlanSelectionAndPlanGiftIsVisibleBeforeSale(t *testing.T) {
	var initial bytes.Buffer
	if err := NewMember(NewMemberProps{}).Render(context.Background(), &initial); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(initial.String(), "정액권 상품은 회원 등록 후 충전에서 선택해 주세요") {
		t.Fatal("initial generic charge must explain where to select a plan")
	}
	var charge bytes.Buffer
	if err := ChargeModal(ChargeModalProps{MemberID: "fixture", MemberName: "가상 고객", Plans: []PlanRow{{ID: "fixture-plan", TierLabel: "30만원 회원권", Price: 300000, CreditAmount: 330000, PaymentMethod: "card", PerkLabel: "홈케어 증정"}}}).Render(context.Background(), &charge); err != nil {
		t.Fatal(err)
	}
	html := charge.String()
	for _, want := range []string{`data-perk="홈케어 증정"`, "증정 · 홈케어 증정", `x-show="pending && pending.perk" x-text="pending && pending.perk"`, `perk: el.dataset.perk ||`} {
		if !strings.Contains(html, want) {
			t.Errorf("gift must remain visible through selection and confirmation: missing %q", want)
		}
	}
}
