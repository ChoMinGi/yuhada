package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	v2dbgen "github.com/mingicho/yuhada/internal/v2/db/dbgen"
	_ "modernc.org/sqlite"
)

func fixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".", "../migrations-v2"} {
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".sql") || file.Name() == "00006_shared_ledger_memberships.sql" {
				continue
			}
			applySQL(t, db, filepath.Join(dir, file.Name()), false)
		}
	}
	_, err = db.Exec(`
INSERT INTO admin_users (id,email,pw_hash,name,role,is_active,created_at,updated_at,pin_hash)
VALUES ('fixture-admin','owner@example.test','fixture-password-hash','Fixture owner','owner',1,'2026-05-01T00:00:00.000Z','2026-05-01T00:00:00.000Z','fixture-pin-hash');
INSERT INTO members (id,name,phone,card_uuid,balance,memo,is_active,created_at,updated_at,kakao_user_id)
VALUES ('fixture-member','enc:fixture-name','enc:fixture-phone','fixture-card',100000,'enc:fixture-memo',1,'2026-05-02T01:00:00.000Z','2026-05-02T01:00:00.000Z','fixture-kakao');
INSERT INTO transactions (id,member_id,type,amount,balance_after,memo,created_by,created_at)
VALUES ('fixture-charge','fixture-member','charge',100000,100000,'Initial fixture charge','fixture-admin','2026-05-02T01:00:00.001Z');
INSERT INTO sessions (token,data,expiry) VALUES ('fixture-v1-session',X'010203',2000000000);
INSERT INTO v2_members (id,name,phone,balance,memo) VALUES ('fixture-demo','Demo member','00000000000',500,'Demo memo');
INSERT INTO v2_transactions (id,member_id,type,amount,balance_after) VALUES ('fixture-demo-charge','fixture-demo','charge',500,500);
INSERT INTO v2_memberships (id,member_id,plan_snapshot,charge_tx_id) VALUES ('fixture-demo-sale','fixture-demo','{"price":500,"credit_amount":500}','fixture-demo-charge');
INSERT INTO v2_sessions (token,data,expiry) VALUES ('fixture-demo-session',X'040506',2000000000);
`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func applySQL(t *testing.T, db *sql.DB, path string, down bool) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(contents), "-- +goose Down", 2)
	sqlText := parts[0]
	if down {
		if len(parts) < 2 {
			t.Fatal("missing Down section")
		}
		sqlText = parts[1]
	}
	if _, err := db.Exec(sqlText); err != nil {
		t.Fatalf("apply %s (down=%v): %v", path, down, err)
	}
}

func tableSnapshot(t *testing.T, db *sql.DB, table string) [][]any {
	t.Helper()
	rows, err := db.Query("SELECT * FROM " + table + " ORDER BY 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var result [][]any
	for rows.Next() {
		values, ptrs := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = append([]byte(nil), b...)
			}
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSharedLedgerMigrationPreservesCanonicalAndDemoRows(t *testing.T) {
	db := fixtureDB(t)
	protected := []string{"members", "transactions", "admin_users", "sessions", "kakao_verify_requests", "v2_members", "v2_transactions", "v2_memberships", "v2_membership_plans", "v2_deduct_amount_presets", "v2_service_memo_presets", "v2_admin_users", "v2_sessions", "v2_kakao_verify_requests"}
	before := map[string][][]any{}
	for _, table := range protected {
		before[table] = tableSnapshot(t, db, table)
	}
	applySQL(t, db, "00006_shared_ledger_memberships.sql", false)
	for _, table := range protected {
		if !reflect.DeepEqual(before[table], tableSnapshot(t, db, table)) {
			t.Errorf("migration changed protected table %s", table)
		}
	}
	for table, want := range map[string]int{"membership_plans": 6, "deduct_amount_presets": 5, "service_memo_presets": 6, "memberships": 0, "admin_sessions_v2": 0} {
		if got := count(t, db, table); got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
	// An operator may have edited or disabled a seeded plan after first launch.
	if _, err := db.Exec("UPDATE membership_plans SET price=210000,is_active=0 WHERE id='plan-20-card'"); err != nil {
		t.Fatal(err)
	}
	seed := tableSnapshot(t, db, "membership_plans")
	applySQL(t, db, "00006_shared_ledger_memberships.sql", false)
	if !reflect.DeepEqual(seed, tableSnapshot(t, db, "membership_plans")) {
		t.Error("reapplying migration rewrote plan seeds")
	}
	for _, table := range protected {
		if !reflect.DeepEqual(before[table], tableSnapshot(t, db, table)) {
			t.Errorf("reapply changed protected table %s", table)
		}
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Error("foreign key violation after migration")
	}
}

func TestSharedLedgerMembershipConstraintsAndNonDestructiveRollback(t *testing.T) {
	db := fixtureDB(t)
	applySQL(t, db, "00006_shared_ledger_memberships.sql", false)
	_, err := db.Exec(`INSERT INTO memberships (id,member_id,plan_id,plan_snapshot,charge_tx_id) VALUES ('fixture-sale','fixture-member','plan-20-card','{"price":200000,"credit_amount":220000}','fixture-charge')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO memberships (id,member_id,plan_snapshot,charge_tx_id) VALUES ('duplicate-sale','fixture-member','{}','fixture-charge')`)
	if err == nil {
		t.Fatal("same canonical charge accepted multiple memberships")
	}
	before := tableSnapshot(t, db, "memberships")
	applySQL(t, db, "00006_shared_ledger_memberships.sql", true)
	if !reflect.DeepEqual(before, tableSnapshot(t, db, "memberships")) {
		t.Fatal("schema Down removed recorded operational sales")
	}
	if _, err := db.Exec("DELETE FROM membership_plans WHERE id='plan-20-card'"); err != nil {
		t.Fatal(err)
	}
	var planID sql.NullString
	var snapshot string
	if err := db.QueryRow("SELECT plan_id,plan_snapshot FROM memberships WHERE id='fixture-sale'").Scan(&planID, &snapshot); err != nil {
		t.Fatal(err)
	}
	if planID.Valid || snapshot != `{"price":200000,"credit_amount":220000}` {
		t.Fatal("removing a plan changed historical sale snapshot")
	}
	// Legacy v1 deletion order must continue to work after a plan sale exists.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM transactions WHERE member_id='fixture-member'"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("DELETE FROM members WHERE id='fixture-member'"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "memberships") != 0 {
		t.Fatal("legacy explicit deletion left orphan sale metadata")
	}
	if count(t, db, "v2_members") != 1 || count(t, db, "v2_transactions") != 1 || count(t, db, "v2_memberships") != 1 {
		t.Fatal("legacy deletion changed demo data")
	}
}

func TestSharedLedgerExpirationNormalizesISOBoundaries(t *testing.T) {
	db := fixtureDB(t)
	applySQL(t, db, "00006_shared_ledger_memberships.sql", false)
	_, err := db.Exec(`INSERT INTO memberships (id,member_id,plan_snapshot,charge_tx_id,purchased_at) VALUES ('fixture-sale','fixture-member','{}','fixture-charge','2025-11-15T01:00:00.000Z')`)
	if err != nil {
		t.Fatal(err)
	}
	q := v2dbgen.New(db)
	for _, tc := range []struct {
		name, from, to string
		want           int
	}{
		{"same date lower boundary", "2026-11-15T00:00:00Z", "2026-11-16T00:00:00Z", 1},
		{"exclusive upper boundary", "2026-11-14T01:00:00Z", "2026-11-15T01:00:00Z", 0},
		{"inclusive lower boundary", "2026-11-15T01:00:00Z", "2026-11-16T01:00:00Z", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := q.ListExpiringMemberships(context.Background(), v2dbgen.ListExpiringMembershipsParams{PurchasedAt: tc.from, PurchasedAt2: tc.to})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.want {
				t.Fatalf("expiration rows = %d, want %d", len(rows), tc.want)
			}
		})
	}
}
