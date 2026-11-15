package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	v2service "github.com/mingicho/yuhada/internal/v2/service"
)

func TestSalesDashboardDisplaysGenericAndInitialSalesWithPlanPrice(t *testing.T) {
	_, h, _, _ := handlerLedgerFixture(t)
	member, err := h.Deps.Services.Member.Create(t.Context(), v2service.CreateMemberInput{Name: "Sales fixture", Phone: "01022225555", InitialCharge: 50000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.Deps.Services.Charge.Charge(t.Context(), member.ID, nil, 70000, "fixture-actor"); err != nil {
		t.Fatal(err)
	}
	plan := "plan-20-cash"
	if _, err = h.Deps.Services.Charge.Charge(t.Context(), member.ID, &plan, 0, "fixture-actor"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.SalesDashboard(w, httptest.NewRequest("GET", "/v2/admin/sales?period=all", nil))
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("sales response %d", w.Code)
	}
	for _, text := range []string{"판매 금액", "₩320,000", "₩360,000", "₩40,000", "+₩200,000", "적립 ₩240,000", "일반 충전 · 결제수단 미상", "₩120,000"} {
		if !strings.Contains(body, text) {
			t.Fatalf("sales dashboard missing %q", text)
		}
	}
	for _, text := range []string{"미확정", "확인된 판매 금액", "판매 금액에 포함하지 않습니다"} {
		if strings.Contains(body, text) {
			t.Fatalf("sales dashboard still has superseded %q", text)
		}
	}
}
