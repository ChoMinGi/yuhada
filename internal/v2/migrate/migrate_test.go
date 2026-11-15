package migrate

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func migrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
func TestUpRejectsCanonicalLedgerBeforeCreatingSchemaOrVersionTable(t *testing.T) {
	for _, table := range []string{"members", "transactions", "MEMBERS"} {
		t.Run(table, func(t *testing.T) {
			db := migrationDB(t)
			if _, err := db.Exec("CREATE TABLE " + table + " (id TEXT PRIMARY KEY); INSERT INTO " + table + " VALUES ('existing-operation')"); err != nil {
				t.Fatal(err)
			}
			// No migrations-v2 directory exists in this package directory. Refusal must
			// occur before filesystem lookup or any goose global/schema changes.
			err := Up(db)
			if err == nil || !strings.Contains(err.Error(), "canonical ledger") {
				t.Fatalf("canonical migration not refused: %v", err)
			}
			var changed, protected int
			if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name LIKE 'v2_%'").Scan(&changed); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT count(*) FROM " + table).Scan(&protected); err != nil {
				t.Fatal(err)
			}
			if changed != 0 || protected != 1 {
				t.Fatalf("guard mutated schema/ledger: %d %d", changed, protected)
			}
		})
	}
}
func TestUpIsolatedDemoDatabaseStillMigratesIdempotently(t *testing.T) {
	t.Chdir("../../..")
	oldTable := goose.TableName()
	t.Cleanup(func() { goose.SetTableName(oldTable) })
	db := migrationDB(t)
	for i := 0; i < 2; i++ {
		if err := Up(db); err != nil {
			t.Fatal(err)
		}
	}
	var plans, members, canonical int
	if err := db.QueryRow("SELECT count(*) FROM v2_membership_plans").Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM v2_members").Scan(&members); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('members','transactions')").Scan(&canonical); err != nil {
		t.Fatal(err)
	}
	if plans != 6 || members != 0 || canonical != 0 {
		t.Fatalf("unexpected demo schema: plans=%d members=%d canonical=%d", plans, members, canonical)
	}
}
