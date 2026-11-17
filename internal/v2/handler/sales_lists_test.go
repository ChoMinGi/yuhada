package handler

import (
	"fmt"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	v2service "github.com/mingicho/yuhada/internal/v2/service"
)

func TestSalesListFiltersAndPagesWithoutLosingOlderCharges(t *testing.T) {
	db, h, _, sender := handlerLedgerFixture(t)
	m, err := h.Deps.Services.Member.Create(t.Context(), v2service.CreateMemberInput{Name: "판매 검증 고객", Phone: "01011221122"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 53; i++ {
		if _, err = db.Exec(`INSERT INTO transactions(id,member_id,type,amount,balance_after,created_at) VALUES(?,?,'charge',220000,0,'2026-10-01T00:00:00.000Z')`, fmt.Sprintf("sale-page-%03d", i), m.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	pattern := regexp.MustCompile(`data-record-id="(sale-page-[0-9]+)"`)
	for i, want := range []int{20, 20, 13} {
		path := fmt.Sprintf("/v2/admin/sales/list?period=all&kind=recent&page=%d", i+1)
		if i > 0 {
			path += "&more=1&start=1"
		}
		r := httptest.NewRequest("GET", path, nil)
		if i > 0 {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		h.SalesList(w, r)
		body := w.Body.String()
		if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("page%d status%d", i+1, w.Code)
		}
		matches := pattern.FindAllStringSubmatch(body, -1)
		if len(matches) != want {
			t.Fatalf("page%d has%d want%d", i+1, len(matches), want)
		}
		for _, match := range matches {
			if seen[match[1]] {
				t.Fatal("duplicate across pages")
			}
			seen[match[1]] = true
		}
		if !strings.Contains(body, "총 53건") || strings.Contains(body, "@component.") {
			t.Fatalf("bad count/template page%d", i+1)
		}
		if i > 0 && (!strings.Contains(body, `hx-swap-oob="beforeend:#sales-list-rows"`) || strings.Contains(body, "<html")) {
			t.Fatal("more did not append a page fragment")
		}
		if i == 2 && strings.Contains(body, "더 보기") {
			t.Fatal("more on finalpage")
		}
	}
	if len(seen) != 53 {
		t.Fatal("lost older sales")
	}
	w := httptest.NewRecorder()
	h.SalesList(w, httptest.NewRequest("GET", "/v2/admin/sales/list?period=all&q=nonexistent", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "조건에 맞는 기록이 없습니다") || strings.Contains(w.Body.String(), "data-record-id=") {
		t.Fatal("empty filter widened population")
	}
	if sender.checks.Load() != 0 || len(sender.sent) != 0 {
		t.Fatal("list read invoked SMS")
	}
}
