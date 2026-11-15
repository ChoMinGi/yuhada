package seed

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mingicho/yuhada/internal/crypto"
	"github.com/mingicho/yuhada/internal/v2/service"
	_ "modernc.org/sqlite"
)

func seedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
func demoSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	files, err := filepath.Glob("../../../migrations-v2/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("missing demo migrations")
	}
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(strings.SplitN(string(contents), "-- +goose Down", 2)[0]); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
}
func TestEnsureSeededRejectsCanonicalLedgerBeforeWriting(t *testing.T) {
	for _, table := range []string{"members", "transactions", "MEMBERS"} {
		t.Run(table, func(t *testing.T) {
			db := seedDB(t)
			demoSchema(t, db)
			if _, err := db.Exec("CREATE TABLE " + table + " (id TEXT PRIMARY KEY); INSERT INTO " + table + " VALUES ('existing-operation')"); err != nil {
				t.Fatal(err)
			}
			err := EnsureSeeded(context.Background(), db, service.New(db, nil))
			if err == nil || !strings.Contains(err.Error(), "canonical ledger") {
				t.Fatalf("canonical seeding was not refused: %v", err)
			}
			var demo, real int
			if err = db.QueryRow("SELECT count(*) FROM v2_members").Scan(&demo); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT count(*) FROM " + table).Scan(&real); err != nil {
				t.Fatal(err)
			}
			if demo != 0 || real != 1 {
				t.Fatalf("guard changed rows: demo=%d real=%d", demo, real)
			}
		})
	}
}
func TestEnsureSeededOnlyWritesIsolatedDemoTableAndIsIdempotent(t *testing.T) {
	db := seedDB(t)
	demoSchema(t, db)
	enc, err := crypto.New([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	s := service.New(db, enc)
	for i := 0; i < 2; i++ {
		if err = EnsureSeeded(context.Background(), db, s); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM v2_members").Scan(&count); err != nil || count != 3 {
		t.Fatalf("seed count=%d err=%v", count, err)
	}
	var name, phone, memo string
	var balance int64
	if err = db.QueryRow("SELECT name,phone,memo,balance FROM v2_members WHERE phone=?", enc.EncryptDeterministic("01000000001")).Scan(&name, &phone, &memo, &balance); err != nil {
		t.Fatal(err)
	}
	if name == "테스트1" || enc.Decrypt(name) != "테스트1" || enc.Decrypt(phone) != "01000000001" || enc.Decrypt(memo) != "v2 시드 회원 (자동 생성)" || balance != 0 {
		t.Fatal("demo encryption/content changed")
	}
	if _, err = db.Exec("CREATE TABLE members (id TEXT)"); err != nil {
		t.Fatal(err)
	}
	// Even populated demo tables do not bypass the canonical guard.
	if err = EnsureSeeded(context.Background(), db, s); err == nil {
		t.Fatal("populated demo bypassed guard")
	}
}
func TestEnsureSeededRollsBackPartialDemoInsert(t *testing.T) {
	db := seedDB(t)
	demoSchema(t, db)
	if _, err := db.Exec(`CREATE TRIGGER reject_second_demo BEFORE INSERT ON v2_members WHEN NEW.phone='01000000002' BEGIN SELECT RAISE(ABORT,'fixture seed failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSeeded(context.Background(), db, service.New(db, nil)); err == nil {
		t.Fatal("expected seed failure")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM v2_members").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial seed persisted %d %v", count, err)
	}
}
