// Package migrate contains deprecated isolated demo-database migrations.
// Shared-ledger startup must use the canonical migrations instead.
package migrate

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/pressly/goose/v3"
)

// Dir — migrations-v2/ 상대 경로. main.go 실행 디렉토리(프로젝트 루트) 기준.
const Dir = "migrations-v2"

// Up applies only the isolated legacy demo schema.
// Deprecated: never call this on an operational canonical ledger database.
func Up(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 migrate: database is required")
	}
	var canonical int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table','view') AND lower(name) IN ('members','transactions')`).Scan(&canonical); err != nil {
		return fmt.Errorf("v2 migrate: inspect schema: %w", err)
	}
	if canonical > 0 {
		return fmt.Errorf("v2 migrate: refusing demo migrations on a canonical ledger database")
	}

	if _, err := os.Stat(Dir); err != nil {
		return fmt.Errorf("v2 migrate: dir %q not accessible: %w", Dir, err)
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("v2 migrate: set dialect: %w", err)
	}
	// Demo migrations have their own version table.
	goose.SetTableName("v2_goose_db_version")
	if err := goose.Up(db, Dir); err != nil {
		return fmt.Errorf("v2 migrate: up: %w", err)
	}
	return nil
}
