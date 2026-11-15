package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mingicho/yuhada/internal/sms"
	v2auth "github.com/mingicho/yuhada/internal/v2/auth"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	_ "modernc.org/sqlite"
)

type ledgerSMS struct {
	checks atomic.Int64
	sent   chan [2]string
}

func (s *ledgerSMS) Enabled() bool { s.checks.Add(1); return true }
func (s *ledgerSMS) Send(phone, message string) error {
	s.sent <- [2]string{phone, message}
	return nil
}
func handlerLedgerFixture(t *testing.T) (*sql.DB, *PageHandlers, http.Handler, *ledgerSMS) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../../../migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(strings.SplitN(string(contents), "-- +goose Down", 2)[0]); err != nil {
			t.Fatal(err)
		}
	}
	sender := &ledgerSMS{sent: make(chan [2]string, 10)}
	sm := v2auth.NewSessionManager(nil, false)
	h := &PageHandlers{Deps: &Deps{Services: v2service.New(db, nil), Session: sm, SMS: sender}}
	router := chi.NewRouter()
	router.Post("/members", h.NewMemberPost)
	router.Post("/members/{id}/charge", h.ChargeSubmit)
	router.Post("/members/{id}/deduct", h.DeductSubmit)
	router.Get("/members/{id}", h.MemberDetailGet)
	wrapped := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), v2auth.SessionKeyRole, v2auth.RoleAdmin)
		sm.Put(r.Context(), v2auth.SessionKeyAdminID, "fixture-actor")
		router.ServeHTTP(w, r)
	}))
	return db, h, wrapped, sender
}
func ledgerPost(t *testing.T, handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func expectSMS(t *testing.T, sender *ledgerSMS, phone, message string) {
	t.Helper()
	select {
	case got := <-sender.sent:
		if got != [2]string{phone, message} {
			t.Fatalf("SMS = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing SMS after successful commit")
	}
}
func TestWalletJSONCanonicalSMSAndFailure(t *testing.T) {
	db, h, router, sender := handlerLedgerFixture(t)
	member, err := h.Deps.Services.Member.Create(t.Context(), v2service.CreateMemberInput{Name: "고객", Phone: "01012345678", InitialCharge: 100000})
	if err != nil {
		t.Fatal(err)
	}
	w := ledgerPost(t, router, "/members/"+member.ID+"/charge", url.Values{"amount": {"50,000"}, "memo": {"일반 충전 메모"}})
	var body struct {
		OK       bool   `json:"ok"`
		Redirect string `json:"redirect"`
		Error    string `json:"error"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || !body.OK || body.Redirect != "/v2/admin/members/"+member.ID {
		t.Fatalf("response %d %s %v", w.Code, w.Body.String(), err)
	}
	expectSMS(t, sender, member.Phone, sms.ChargeMessage(member.Name, 50000, 150000))
	var memo, actor string
	if err = db.QueryRow("SELECT memo,created_by FROM transactions WHERE amount=50000").Scan(&memo, &actor); err != nil || memo != "일반 충전 메모" || actor != "fixture-actor" {
		t.Fatalf("metadata %q %q %v", memo, actor, err)
	}
	w = ledgerPost(t, router, "/members/"+member.ID+"/deduct", url.Values{"amount": {"23000"}, "service_name": {"커트"}})
	if w.Code != 200 {
		t.Fatalf("deduct %d %s", w.Code, w.Body.String())
	}
	expectSMS(t, sender, member.Phone, sms.DeductMessage(member.Name, "커트", 23000, 127000))
	w = ledgerPost(t, router, "/members/"+member.ID+"/charge", url.Values{"plan_id": {"plan-20-cash"}, "memo": {"현금 회원권"}})
	if w.Code != 200 {
		t.Fatalf("plan charge %d %s", w.Code, w.Body.String())
	}
	expectSMS(t, sender, member.Phone, sms.ChargeMessage(member.Name, 240000, 367000))

	checks := sender.checks.Load()
	w = ledgerPost(t, router, "/members/"+member.ID+"/deduct", url.Values{"amount": {"367001"}, "service_name": {"커트"}})
	if w.Code != 422 || !strings.Contains(w.Body.String(), "잔액이 부족") || sender.checks.Load() != checks {
		t.Fatalf("insufficient %d %s", w.Code, w.Body.String())
	}
	if _, err = db.Exec(`CREATE TRIGGER reject_wallet BEFORE INSERT ON transactions BEGIN SELECT RAISE(ABORT,'fixture wallet failure'); END`); err != nil {
		t.Fatal(err)
	}
	w = ledgerPost(t, router, "/members/"+member.ID+"/charge", url.Values{"amount": {"10000"}})
	if w.Code != 500 || sender.checks.Load() != checks {
		t.Fatalf("failed commit sent SMS %d %s", w.Code, w.Body.String())
	}
	got, err := h.Deps.Services.Member.Get(t.Context(), member.ID)
	if err != nil || got.Balance != 367000 {
		t.Fatalf("failed mutation persisted %+v %v", got, err)
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM transactions").Scan(&n); err != nil || n != 4 {
		t.Fatalf("transaction count %d %v", n, err)
	}
}
func TestSignupInitialChargeUsesFirstChargeSMS(t *testing.T) {
	db, h, router, sender := handlerLedgerFixture(t)
	w := ledgerPost(t, router, "/members", url.Values{"name": {"신규 고객"}, "phone": {"010-9876-5432"}, "memo": {"회원 메모"}, "amount": {"200,000"}})
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/v2/admin/members/") {
		t.Fatalf("signup %d %s", w.Code, w.Body.String())
	}
	id := strings.TrimPrefix(w.Header().Get("Location"), "/v2/admin/members/")
	member, err := h.Deps.Services.Member.Get(t.Context(), id)
	if err != nil || member.Balance != 200000 || member.Memo.String != "회원 메모" {
		t.Fatalf("signup %+v %v", member, err)
	}
	expectSMS(t, sender, member.Phone, sms.FirstChargeMessage(member.Name, 200000, 200000))
	checks := sender.checks.Load()
	if _, err = db.Exec(`CREATE TRIGGER reject_signup BEFORE INSERT ON transactions BEGIN SELECT RAISE(ABORT,'fixture signup failure'); END`); err != nil {
		t.Fatal(err)
	}
	w = ledgerPost(t, router, "/members", url.Values{"name": {"등록 실패"}, "phone": {"01098765433"}, "amount": {"30000"}})
	if w.Code != 422 || sender.checks.Load() != checks {
		t.Fatalf("failed signup sent SMS %d", w.Code)
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM members").Scan(&n); err != nil || n != 1 {
		t.Fatalf("failed signup left member %d %v", n, err)
	}
}
