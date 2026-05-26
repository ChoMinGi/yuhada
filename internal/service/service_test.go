package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mingicho/yuhada/internal/crypto"

	_ "modernc.org/sqlite"
)

// testDB — 테스트용 in-memory SQLite DB + 마이그레이션.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// 마이그레이션 직접 실행 (goose 마커 파싱: -- +goose Up 이후 ~ -- +goose Down 이전)
	migrationsDir := filepath.Join("..", "..", "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".sql" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(migrationsDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		upSQL := extractGooseUp(string(data))
		if upSQL == "" {
			continue
		}
		if _, err := db.Exec(upSQL); err != nil {
			t.Fatalf("exec %s: %v", e.Name(), err)
		}
	}
	return db
}

// extractGooseUp — goose SQL 파일에서 Up 부분만 추출.
func extractGooseUp(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	inUp := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "-- +goose Up" {
			inUp = true
			continue
		}
		if trimmed == "-- +goose Down" {
			break
		}
		if trimmed == "-- +goose StatementBegin" || trimmed == "-- +goose StatementEnd" {
			continue
		}
		if inUp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func testServices(t *testing.T) *Services {
	t.Helper()
	return New(testDB(t), nil)
}

func testServicesWithEnc(t *testing.T) *Services {
	t.Helper()
	enc, err := crypto.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return New(testDB(t), enc)
}

// ─────────────────────────────────────────────
// Member
// ─────────────────────────────────────────────

func TestMemberCreate(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, err := s.Member.Create(ctx, CreateMemberInput{
		Name:  "김지영",
		Phone: "010-1234-5678",
		Memo:  "VIP",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "김지영" {
		t.Errorf("name = %q, want 김지영", m.Name)
	}
	if m.Phone != "01012345678" {
		t.Errorf("phone = %q, want 01012345678", m.Phone)
	}
	if m.Balance != 0 {
		t.Errorf("balance = %d, want 0", m.Balance)
	}
}

func TestMemberCreateDuplicatePhone(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	s.Member.Create(ctx, CreateMemberInput{Name: "A", Phone: "01011111111"})
	_, err := s.Member.Create(ctx, CreateMemberInput{Name: "B", Phone: "01011111111"})
	if err != ErrDuplicatePhone {
		t.Errorf("got %v, want ErrDuplicatePhone", err)
	}
}

func TestMemberCreateValidation(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	_, err := s.Member.Create(ctx, CreateMemberInput{Name: "", Phone: "01011111111"})
	if err == nil {
		t.Error("empty name should fail")
	}
	_, err = s.Member.Create(ctx, CreateMemberInput{Name: "A", Phone: ""})
	if err == nil {
		t.Error("empty phone should fail")
	}
}

func TestMemberGet(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	created, _ := s.Member.Create(ctx, CreateMemberInput{Name: "김지영", Phone: "01011111111"})
	got, err := s.Member.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "김지영" {
		t.Errorf("name = %q", got.Name)
	}
}

func TestMemberGetNotFound(t *testing.T) {
	s := testServices(t)
	_, err := s.Member.Get(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestMemberSearch(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	s.Member.Create(ctx, CreateMemberInput{Name: "김지영", Phone: "01011111111"})
	s.Member.Create(ctx, CreateMemberInput{Name: "박향신", Phone: "01022222222"})
	s.Member.Create(ctx, CreateMemberInput{Name: "김민수", Phone: "01033333333"})

	results, err := s.Member.Search(ctx, "김", SortDefault)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("search '김' got %d results, want 2", len(results))
	}

	// phone search
	results, err = s.Member.Search(ctx, "0102", SortDefault)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("search '0102' got %d results, want 1", len(results))
	}

	// empty query → all
	results, _ = s.Member.Search(ctx, "", SortDefault)
	if len(results) != 3 {
		t.Errorf("empty search got %d, want 3", len(results))
	}
}

func TestMemberSearchSort(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m1, _ := s.Member.Create(ctx, CreateMemberInput{Name: "A", Phone: "01011111111"})
	m2, _ := s.Member.Create(ctx, CreateMemberInput{Name: "B", Phone: "01022222222"})

	s.Wallet.Charge(ctx, m1.ID, 100000, "", "")
	s.Wallet.Charge(ctx, m2.ID, 200000, "", "")

	results, _ := s.Member.Search(ctx, "", SortBalanceDesc)
	if results[0].Balance != 200000 {
		t.Error("SortBalanceDesc should put highest first")
	}

	results, _ = s.Member.Search(ctx, "", SortBalanceAsc)
	if results[0].Balance != 100000 {
		t.Error("SortBalanceAsc should put lowest first")
	}
}

func TestMemberDelete(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "삭제대상", Phone: "01099999999"})
	s.Wallet.Charge(ctx, m.ID, 50000, "", "")

	if err := s.Member.Delete(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.Member.Get(ctx, m.ID)
	if err != ErrNotFound {
		t.Error("deleted member should not be found")
	}
}

func TestMemberCardLifecycle(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "카드테스트", Phone: "01088888888"})
	s.Wallet.Charge(ctx, m.ID, 100000, "", "")

	// Issue card
	s.Member.IssueCard(ctx, m.ID, "CARD-0001")
	got, _ := s.Member.Get(ctx, m.ID)
	if got.CardUuid.String != "CARD-0001" {
		t.Errorf("card = %q", got.CardUuid.String)
	}

	// Report lost
	s.Member.ReportLost(ctx, m.ID)
	got, _ = s.Member.Get(ctx, m.ID)
	if got.IsActive {
		t.Error("should be inactive after lost")
	}

	// Can't charge inactive
	_, err := s.Wallet.Charge(ctx, m.ID, 10000, "", "")
	if err != ErrInactive {
		t.Errorf("charge inactive should fail, got %v", err)
	}

	// Reactivate
	s.Member.Reactivate(ctx, m.ID)
	got, _ = s.Member.Get(ctx, m.ID)
	if !got.IsActive {
		t.Error("should be active after reactivate")
	}
}

func TestMemberGetByCardUUID(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "카드조회", Phone: "01077777777"})
	s.Member.IssueCard(ctx, m.ID, "CARD-TEST")

	got, err := s.Member.GetByCardUUID(ctx, "CARD-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "카드조회" {
		t.Errorf("name = %q", got.Name)
	}

	// 존재하지 않는 카드
	_, err = s.Member.GetByCardUUID(ctx, "CARD-NONE")
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}

	// 분실 카드는 조회 안 됨 (is_active=0)
	s.Member.ReportLost(ctx, m.ID)
	_, err = s.Member.GetByCardUUID(ctx, "CARD-TEST")
	if err != ErrNotFound {
		t.Errorf("lost card should return ErrNotFound, got %v", err)
	}
}

func TestMemberUpdateMemo(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "메모", Phone: "01066666666"})

	s.Member.UpdateMemo(ctx, m.ID, "VIP 고객")
	got, _ := s.Member.Get(ctx, m.ID)
	if got.Memo.String != "VIP 고객" {
		t.Errorf("memo = %q, want VIP 고객", got.Memo.String)
	}

	// 빈 메모로 초기화
	s.Member.UpdateMemo(ctx, m.ID, "")
	got, _ = s.Member.Get(ctx, m.ID)
	if got.Memo.Valid {
		t.Error("empty memo should be NULL")
	}
}

func TestMemberMigrateEncryption(t *testing.T) {
	// 평문 DB → 암호화 마이그레이션
	db := testDB(t)
	plainSvc := New(db, nil)
	ctx := context.Background()

	plainSvc.Member.Create(ctx, CreateMemberInput{Name: "평문회원", Phone: "01011112222"})

	// DB에 평문으로 저장되었는지 확인
	var rawName string
	db.QueryRow("SELECT name FROM members LIMIT 1").Scan(&rawName)
	if rawName != "평문회원" {
		t.Fatalf("expected plaintext, got %q", rawName)
	}

	// 암호화 서비스로 전환 후 마이그레이션
	enc, _ := crypto.New([]byte("0123456789abcdef0123456789abcdef"))
	encSvc := New(db, enc)
	if err := encSvc.Member.MigrateEncryption(ctx); err != nil {
		t.Fatal(err)
	}

	// DB에 암호화되었는지 확인
	db.QueryRow("SELECT name FROM members LIMIT 1").Scan(&rawName)
	if !crypto.IsEncrypted(rawName) {
		t.Errorf("expected encrypted, got %q", rawName)
	}

	// 복호화 조회 정상
	members, _ := encSvc.Member.Search(ctx, "", SortDefault)
	if len(members) != 1 || members[0].Name != "평문회원" {
		t.Errorf("decrypted name = %q, want 평문회원", members[0].Name)
	}

	// 이미 암호화된 상태에서 재실행 → 에러 없이 스킵
	if err := encSvc.Member.MigrateEncryption(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMemberEncryptedPhoneSearch(t *testing.T) {
	s := testServicesWithEnc(t)
	ctx := context.Background()

	s.Member.Create(ctx, CreateMemberInput{Name: "김지영", Phone: "01012345678"})
	s.Member.Create(ctx, CreateMemberInput{Name: "박향신", Phone: "01099998888"})

	// 전화번호로 검색
	results, err := s.Member.Search(ctx, "0101234", SortDefault)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Name != "김지영" {
		t.Errorf("phone search got %d results", len(results))
	}
}

// ─────────────────────────────────────────────
// Wallet
// ─────────────────────────────────────────────

func TestWalletCharge(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "충전", Phone: "01011111111"})
	res, err := s.Wallet.Charge(ctx, m.ID, 100000, "현금", "admin1")
	if err != nil {
		t.Fatal(err)
	}
	if res.NewBalance != 100000 {
		t.Errorf("balance = %d, want 100000", res.NewBalance)
	}
	if res.Transaction.Type != "charge" {
		t.Errorf("tx type = %q", res.Transaction.Type)
	}
}

func TestWalletDeduct(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "차감", Phone: "01011111111"})
	s.Wallet.Charge(ctx, m.ID, 100000, "", "")

	res, err := s.Wallet.Deduct(ctx, m.ID, 30000, "디자이너컷", "admin1")
	if err != nil {
		t.Fatal(err)
	}
	if res.NewBalance != 70000 {
		t.Errorf("balance = %d, want 70000", res.NewBalance)
	}
}

func TestWalletDeductInsufficient(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "잔액부족", Phone: "01011111111"})
	s.Wallet.Charge(ctx, m.ID, 50000, "", "")

	_, err := s.Wallet.Deduct(ctx, m.ID, 100000, "", "")
	if err == nil {
		t.Error("should fail with insufficient balance")
	}

	// balance unchanged
	got, _ := s.Member.Get(ctx, m.ID)
	if got.Balance != 50000 {
		t.Errorf("balance should be unchanged, got %d", got.Balance)
	}
}

func TestWalletZeroAmount(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "제로", Phone: "01011111111"})
	_, err := s.Wallet.Charge(ctx, m.ID, 0, "", "")
	if err == nil {
		t.Error("zero amount should fail")
	}
}

func TestWalletNotFound(t *testing.T) {
	s := testServices(t)
	_, err := s.Wallet.Charge(context.Background(), "nonexistent", 10000, "", "")
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// ─────────────────────────────────────────────
// Encryption integration
// ─────────────────────────────────────────────

func TestMemberEncryption(t *testing.T) {
	s := testServicesWithEnc(t)
	ctx := context.Background()

	m, err := s.Member.Create(ctx, CreateMemberInput{
		Name:  "암호화테스트",
		Phone: "01055555555",
		Memo:  "비밀메모",
	})
	if err != nil {
		t.Fatal(err)
	}
	// returned member should be decrypted
	if m.Name != "암호화테스트" {
		t.Errorf("returned name = %q, want 암호화테스트", m.Name)
	}

	// raw DB should be encrypted
	var rawName string
	s.DB.QueryRow("SELECT name FROM members WHERE id = ?", m.ID).Scan(&rawName)
	if rawName == "암호화테스트" {
		t.Error("raw DB name should be encrypted, not plaintext")
	}
	if !crypto.IsEncrypted(rawName) {
		t.Error("raw DB name should have enc: prefix")
	}

	// Get should return decrypted
	got, _ := s.Member.Get(ctx, m.ID)
	if got.Name != "암호화테스트" {
		t.Errorf("Get name = %q", got.Name)
	}

	// Search should work on decrypted data
	results, _ := s.Member.Search(ctx, "암호화", SortDefault)
	if len(results) != 1 {
		t.Errorf("encrypted search got %d results, want 1", len(results))
	}
}

// ─────────────────────────────────────────────
// Admin
// ─────────────────────────────────────────────

func TestAdminBootstrapAndVerifyPIN(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	err := s.Admin.Bootstrap(ctx, "admin@test.com", "123456", "")
	if err != nil {
		t.Fatal(err)
	}

	admin, err := s.Admin.VerifyPIN(ctx, "123456")
	if err != nil {
		t.Fatal(err)
	}
	if admin.Email != "admin@test.com" {
		t.Errorf("email = %q", admin.Email)
	}

	// wrong PIN
	_, err = s.Admin.VerifyPIN(ctx, "000000")
	if err != ErrPasswordInvalid {
		t.Errorf("wrong PIN: got %v, want ErrPasswordInvalid", err)
	}

	// invalid format
	_, err = s.Admin.VerifyPIN(ctx, "abc")
	if err != ErrPasswordInvalid {
		t.Errorf("invalid PIN format: got %v, want ErrPasswordInvalid", err)
	}
}

// ─────────────────────────────────────────────
// Transaction listing
// ─────────────────────────────────────────────

func TestTransactionList(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "거래", Phone: "01011111111"})
	s.Wallet.Charge(ctx, m.ID, 100000, "충전1", "")
	s.Wallet.Deduct(ctx, m.ID, 30000, "컷", "")
	s.Wallet.Charge(ctx, m.ID, 50000, "충전2", "")

	txs, err := s.Tx.ListByMember(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 3 {
		t.Errorf("got %d transactions, want 3", len(txs))
	}

	recent, err := s.Tx.ListRecent(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Errorf("got %d recent, want 2", len(recent))
	}
}

func TestTransactionListInPeriod(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "기간", Phone: "01011111111"})
	s.Wallet.Charge(ctx, m.ID, 100000, "", "")
	s.Wallet.Deduct(ctx, m.ID, 30000, "컷", "")

	// 전체 기간
	rows, err := s.Tx.ListInPeriod(ctx, TxFilter{Period: PeriodAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("PeriodAll got %d, want 2", len(rows))
	}

	// 타입 필터: charge만
	rows, _ = s.Tx.ListInPeriod(ctx, TxFilter{Period: PeriodAll, Type: "charge"})
	if len(rows) != 1 {
		t.Errorf("charge filter got %d, want 1", len(rows))
	}

	// 타입 필터: deduct만
	rows, _ = s.Tx.ListInPeriod(ctx, TxFilter{Period: PeriodAll, Type: "deduct"})
	if len(rows) != 1 {
		t.Errorf("deduct filter got %d, want 1", len(rows))
	}
}

func TestSumsInPeriod(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "합계", Phone: "01011111111"})
	s.Wallet.Charge(ctx, m.ID, 200000, "", "")
	s.Wallet.Charge(ctx, m.ID, 100000, "", "")
	s.Wallet.Deduct(ctx, m.ID, 50000, "", "")

	deduct, charge, err := s.Tx.SumsInPeriod(ctx, TxFilter{Period: PeriodAll})
	if err != nil {
		t.Fatal(err)
	}
	if charge != 300000 {
		t.Errorf("charge sum = %d, want 300000", charge)
	}
	if deduct != 50000 {
		t.Errorf("deduct sum = %d, want 50000", deduct)
	}
}

func TestPeriodRange(t *testing.T) {
	now := time.Date(2026, 5, 26, 14, 30, 0, 0, time.UTC)

	start, _ := PeriodToday.Range(now)
	if start != "2026-05-26T00:00:00.000Z" {
		t.Errorf("today start = %q", start)
	}

	start, _ = PeriodMonth.Range(now)
	if start != "2026-05-01T00:00:00.000Z" {
		t.Errorf("month start = %q", start)
	}

	start, _ = PeriodAll.Range(now)
	if start != "2020-01-01T00:00:00.000Z" {
		t.Errorf("all start = %q", start)
	}

	// 월요일 시작 확인 (5/26 = 화요일)
	start, _ = PeriodWeek.Range(now)
	if start != "2026-05-25T00:00:00.000Z" {
		t.Errorf("week start = %q, want Monday 5/25", start)
	}
}

func TestDashboard(t *testing.T) {
	s := testServices(t)
	ctx := context.Background()

	m, _ := s.Member.Create(ctx, CreateMemberInput{Name: "통계", Phone: "01011111111"})
	s.Wallet.Charge(ctx, m.ID, 200000, "", "")
	s.Wallet.Deduct(ctx, m.ID, 50000, "", "")

	snap, err := s.Stats.Dashboard(ctx, PeriodAll)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ChargeTotal != 200000 {
		t.Errorf("charge total = %d, want 200000", snap.ChargeTotal)
	}
	if snap.DeductTotal != 50000 {
		t.Errorf("deduct total = %d, want 50000", snap.DeductTotal)
	}
	if snap.MemberCount != 1 {
		t.Errorf("member count = %d, want 1", snap.MemberCount)
	}
	if snap.TotalLiability != 150000 {
		t.Errorf("total liability = %d, want 150000", snap.TotalLiability)
	}
}
