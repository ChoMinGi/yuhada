package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	auth1 "github.com/mingicho/yuhada/internal/auth"
	handler1 "github.com/mingicho/yuhada/internal/handler"
	service1 "github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/sms"
	auth2 "github.com/mingicho/yuhada/internal/v2/auth"
	service2 "github.com/mingicho/yuhada/internal/v2/service"
)

func TestBothFrontendsObserveOneLedgerAndCustomerBalance(t *testing.T) {
	db, h, _, _ := handlerLedgerFixture(t)
	legacy := service1.New(db, nil)
	s1, s2 := auth1.NewSessionManager(db, false), auth2.NewSessionManager(db, false)
	sender := sms.New("", "", "")
	router := chi.NewRouter()
	router.Mount("/", handler1.NewRouter(&handler1.Deps{Services: legacy, Session: s1, SMS: sender}))
	router.Mount("/v2", NewRouter(&Deps{Services: h.Deps.Services, Legacy: legacy, Session: s2, SMS: sender}))
	m, err := h.Deps.Services.Member.Create(t.Context(), service2.CreateMemberInput{Name: "가상 통합 고객", Phone: "01000000001", CardUUID: "fictional-shared-card", InitialCharge: 100000})
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRecorder()
	s2.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s2.Put(r.Context(), auth2.SessionKeyRole, auth2.RoleAdmin)
		s2.Put(r.Context(), auth2.SessionKeyKakaoID, "fixture-kakao")
		s2.Put(r.Context(), auth2.SessionKeyAdminID, "fixture-admin")
		w.WriteHeader(204)
	})).ServeHTTP(login, httptest.NewRequest("GET", "/v2/login", nil))
	request := func(method, path string, form url.Values, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if method == "POST" {
			r.Header.Set("Accept", "application/json")
		}
		if authenticated {
			for _, cookie := range login.Result().Cookies() {
				r.AddCookie(cookie)
			}
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	w := request("POST", "/v2/admin/members/"+m.ID+"/charge", url.Values{"amount": {"10000"}, "memo": {"공유 원장 메모"}}, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("v2 charge %d %s", w.Code, w.Body.String())
	}
	old, err := legacy.Member.Get(t.Context(), m.ID)
	if err != nil || old.Balance != 110000 {
		t.Fatalf("v1 cannot see v2 charge: %d %v", old.Balance, err)
	}
	w = request("GET", "/m/fictional-shared-card", nil, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "110,000") {
		t.Fatalf("customer NFC stale: %d", w.Code)
	}
	if _, err = legacy.Wallet.Deduct(context.Background(), m.ID, 20000, "기존 화면 차감", "fixture-admin"); err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/v2/admin/members/"+m.ID, nil, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "90,000") || !strings.Contains(w.Body.String(), "기존 화면 차감") {
		t.Fatalf("v2 detail stale: %d", w.Code)
	}
	for _, path := range []string{"/v2/admin/cards", "/v2/admin/transactions?period=all&q=가상", "/v2/admin/members/" + m.ID + "/lost/modal", "/v2/admin/members/" + m.ID + "/reactivate/modal", "/v2/admin/members/" + m.ID + "/delete/modal"} {
		if got := request("GET", path, nil, true); got.Code != 200 {
			t.Errorf("existing workflow %s: %d", path, got.Code)
		}
	}
	if got := request("POST", "/v2/admin/members/"+m.ID+"/lost", nil, true); got.Code != 204 {
		t.Fatalf("lost: %d", got.Code)
	}
	w = request("GET", "/m/fictional-shared-card", nil, false)
	if !strings.Contains(w.Body.String(), "분실") {
		t.Fatal("old customer URL did not reflect card loss")
	}
	if got := request("POST", "/v2/admin/members/"+m.ID+"/reactivate", nil, true); got.Code != 204 {
		t.Fatalf("reactivate: %d", got.Code)
	}
	old, err = legacy.Member.Get(t.Context(), m.ID)
	if err != nil || !old.IsActive {
		t.Fatal("v1 did not observe reactivation")
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM transactions WHERE member_id=?", m.ID).Scan(&n); err != nil || n != 3 {
		t.Fatalf("lost or duplicated ledger records: %d %v", n, err)
	}
}

func TestNeitherFrontendAcceptsAdministratorPIN(t *testing.T) {
	db, h, _, _ := handlerLedgerFixture(t)
	legacy := service1.New(db, nil)
	if err := legacy.Admin.Bootstrap(t.Context(), "fixture-admin@example.com", "123456", ""); err != nil {
		t.Fatal(err)
	}
	sm := auth2.NewSessionManager(db, false)
	h.Deps.Legacy = legacy
	h.Deps.Session = sm
	r := chi.NewRouter()
	r.Mount("/v2", NewRouter(h.Deps))
	r.Mount("/", handler1.NewRouter(&handler1.Deps{Session: auth1.NewSessionManager(db, false), Services: legacy, SMS: sms.New("", "", "")}))
	post := func(path, pin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(url.Values{"pin": {pin}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/login", "/v2/login"} {
		for _, pin := range []string{"123456", "654321", "0000", ""} {
			if w := post(path, pin); w.Code != 405 {
				t.Errorf("retired PIN login %s returned %d", path, w.Code)
			}
		}
	}
}
