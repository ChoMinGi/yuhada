package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mingicho/yuhada/internal/crypto"
	legacy "github.com/mingicho/yuhada/internal/service"
	_ "modernc.org/sqlite"
)

func sharedFixture(t *testing.T) (*sql.DB, *Services, *legacy.Services) {
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
		up := strings.SplitN(string(contents), "-- +goose Down", 2)[0]
		if _, err = db.Exec(up); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	if _, err = db.Exec(`CREATE TABLE v2_members (id TEXT PRIMARY KEY,balance INTEGER); CREATE TABLE v2_transactions(id TEXT PRIMARY KEY,amount INTEGER); INSERT INTO v2_members VALUES ('demo',94); INSERT INTO v2_transactions VALUES ('demo-tx',94)`); err != nil {
		t.Fatal(err)
	}
	enc, err := crypto.New([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	return db, New(db, enc), legacy.New(db, enc)
}
func fixtureMember(t *testing.T, s *Services) string {
	t.Helper()
	m, err := s.Member.Create(context.Background(), CreateMemberInput{Name: "기존 고객", Phone: "010-1234-5678", CardUUID: "existing-card", Memo: "회원 메모"})
	if err != nil {
		t.Fatal(err)
	}
	return m.ID
}
func assertBalance(t *testing.T, s *Services, id string, want int64) {
	t.Helper()
	m, err := s.Member.Get(context.Background(), id)
	if err != nil || m.Balance != want {
		t.Fatalf("balance=%d want=%d err=%v", m.Balance, want, err)
	}
}
func tableCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCanonicalWalletBothDirectionsAndExistingCard(t *testing.T) {
	db, s, old := sharedFixture(t)
	ctx := context.Background()
	m, err := old.Member.Create(ctx, legacy.CreateMemberInput{Name: "실제 회원", Phone: "01011112222", CardUUID: "old-card", Memo: "기존 메모"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Wallet.Charge(ctx, m.ID, 100000, "기존 충전", "old-admin"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Member.Get(ctx, m.ID)
	if err != nil || got.Name != m.Name || got.Phone != m.Phone || got.Balance != 100000 || got.Memo.String != "기존 메모" {
		t.Fatalf("v1 member not visible: %+v %v", got, err)
	}
	charge, err := s.Charge.ChargeWithMemo(ctx, m.ID, nil, 50000, "  직접 충전 메모  ", "kakao:42")
	if err != nil {
		t.Fatal(err)
	}
	if charge.Transaction.Memo.String != "직접 충전 메모" || charge.Transaction.CreatedBy.String != "kakao:42" {
		t.Fatalf("lost metadata: %+v", charge.Transaction)
	}
	deduction, err := s.Charge.Deduct(ctx, m.ID, 23000, "커트", "new-admin")
	if err != nil || deduction.NewBalance != 127000 {
		t.Fatalf("deduct %+v %v", deduction, err)
	}
	card, err := old.Member.GetByCardUUID(ctx, "old-card")
	if err != nil || card.ID != m.ID || card.Balance != 127000 {
		t.Fatalf("existing customer card changed: %+v %v", card, err)
	}
	if tableCount(t, db, "transactions") != 3 || tableCount(t, db, "memberships") != 0 {
		t.Fatal("generic wallet changed sale metadata")
	}
	var demoBalance int
	if err = db.QueryRow("SELECT balance FROM v2_members WHERE id='demo'").Scan(&demoBalance); err != nil || demoBalance != 94 || tableCount(t, db, "v2_transactions") != 1 {
		t.Fatal("test ledger changed")
	}
}
func TestPlanSaleSnapshotAndRollback(t *testing.T) {
	db, s, _ := sharedFixture(t)
	ctx := context.Background()
	id := fixtureMember(t, s)
	plan := "plan-20-card"
	result, err := s.Charge.ChargeWithMemo(ctx, id, &plan, 999, "상품 구매 메모", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if result.NewBalance != 220000 || result.Transaction.Amount != 220000 || result.MembershipID == "" {
		t.Fatalf("sale %+v", result)
	}
	if _, err = db.Exec("UPDATE membership_plans SET price=900000,credit_amount=990000 WHERE id=?", plan); err != nil {
		t.Fatal(err)
	}
	summary, err := s.Sales.SalesSummary(ctx, "1970", "")
	if err != nil || summary.RealRevenue != 200000 || summary.CreditTotal != 220000 {
		t.Fatalf("snapshot changed %+v %v", summary, err)
	}
	if _, err = db.Exec(`CREATE TRIGGER reject_sale BEFORE INSERT ON memberships BEGIN SELECT RAISE(ABORT,'fixture sale failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Charge.Charge(ctx, id, &plan, 0, "admin"); err == nil {
		t.Fatal("expected atomic sale failure")
	}
	assertBalance(t, s, id, 220000)
	if tableCount(t, db, "transactions") != 1 || tableCount(t, db, "memberships") != 1 {
		t.Fatal("failed sale left rows")
	}
	if _, err = db.Exec("DROP TRIGGER reject_sale; UPDATE membership_plans SET is_active=0 WHERE id='plan-20-card'"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Charge.Charge(ctx, id, &plan, 0, "admin"); !errors.Is(err, ErrPlanInactive) {
		t.Fatalf("inactive plan: %v", err)
	}
	if _, err = s.Charge.Deduct(ctx, id, 220001, "컷", "admin"); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("insufficient: %v", err)
	}
	assertBalance(t, s, id, 220000)
	if tableCount(t, db, "transactions") != 1 {
		t.Fatal("failure added transaction")
	}
}
func TestInitialChargeAtomicAndShared(t *testing.T) {
	db, s, old := sharedFixture(t)
	ctx := context.Background()
	m, err := s.Member.Create(ctx, CreateMemberInput{Name: "신규", Phone: "01098765432", InitialCharge: 50000, CreatedBy: "actor", Memo: "등록 메모"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := old.Member.Get(ctx, m.ID)
	if err != nil || got.Balance != 50000 || got.Memo.String != "등록 메모" {
		t.Fatalf("initial charge invisible %+v %v", got, err)
	}
	var memo, actor string
	if err = db.QueryRow("SELECT memo,created_by FROM transactions WHERE member_id=?", m.ID).Scan(&memo, &actor); err != nil || memo != "최초 충전" || actor != "actor" {
		t.Fatalf("initial metadata %q %q %v", memo, actor, err)
	}
	if _, err = db.Exec(`CREATE TRIGGER reject_initial BEFORE INSERT ON transactions BEGIN SELECT RAISE(ABORT,'fixture initial failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Member.Create(ctx, CreateMemberInput{Name: "실패", Phone: "01098765433", InitialCharge: 20000}); err == nil {
		t.Fatal("initial charge should fail")
	}
	if tableCount(t, db, "members") != 1 || tableCount(t, db, "transactions") != 1 {
		t.Fatal("partial registration persisted")
	}
}
func TestLifetimeTotalsAndAllChargeSalesHaveNoDisplayLimit(t *testing.T) {
	_, s, _ := sharedFixture(t)
	ctx := context.Background()
	id := fixtureMember(t, s)
	for i := 0; i < 605; i++ {
		if _, err := s.Charge.Charge(ctx, id, nil, 1000, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Charge.Deduct(ctx, id, 5000, "컷", "admin"); err != nil {
		t.Fatal(err)
	}
	totals, err := s.Queries.SumMemberTotals(ctx, id)
	if err != nil || totals.TotalIn != 605000 || totals.TotalOut != 5000 {
		t.Fatalf("lifetime totals %+v %v", totals, err)
	}
	rows, err := s.Queries.ListMemberHistoryWithPlan(ctx, id)
	if err != nil || len(rows) != 50 {
		t.Fatalf("history %d %v", len(rows), err)
	}
	ledger, err := s.Sales.LedgerTotals(ctx, "1970", "")
	if err != nil || ledger.Charge != 605000 || ledger.Deduct != 5000 {
		t.Fatalf("ledger %+v %v", ledger, err)
	}
	summary, err := s.Sales.SalesSummary(ctx, "1970", "")
	if err != nil || summary.RealRevenue != 605000 || summary.TxCount != 605 || summary.CreditTotal != 605000 || summary.AvgTicket != 1000 {
		t.Fatalf("all charge sales %+v %v", summary, err)
	}
	top, err := s.Sales.TopMembers(ctx, "1970", "", 5)
	if err != nil || len(top) != 1 || top[0].TotalPrice != 605000 || top[0].Count != 605 {
		t.Fatalf("all charge ranking %+v %v", top, err)
	}
	daily, err := s.Sales.DailyRevenue(ctx, "1970", "")
	if err != nil || len(daily) != 1 || daily[0].Revenue != 605000 {
		t.Fatalf("all charge revenue chart %+v %v", daily, err)
	}
}
