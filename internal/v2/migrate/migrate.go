// Package migrate — v2 마이그레이션 in-process 실행.
//
// R7 대응: main.go 에서 v2 DB open 직후 seed 이전에 호출해 idempotent 하게 최신 스키마 보장.
//
// dev-only (옵션 C 정책, isolation-rules.md §6) 로만 호출되므로 파일시스템 경로
// migrations-v2/ 를 직접 읽는다. 마이그 파일은 IF NOT EXISTS + INSERT OR IGNORE 로
// 이미 idempotent (00001_init.sql / 00002_seed_plans.sql 참조).
package migrate

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/pressly/goose/v3"
)

// Dir — migrations-v2/ 상대 경로. main.go 실행 디렉토리(프로젝트 루트) 기준.
const Dir = "migrations-v2"

// Up — v2 DB 에 latest 마이그레이션 적용. 이미 최신이면 no-op.
// dir 이 없으면 에러 반환 (dev 환경에서 이례적 상황).
func Up(db *sql.DB) error {
	if _, err := os.Stat(Dir); err != nil {
		return fmt.Errorf("v2 migrate: dir %q not accessible: %w", Dir, err)
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("v2 migrate: set dialect: %w", err)
	}
	// v1 과 같은 DB 파일을 공유해도 버전 트래킹이 섞이지 않도록 별도 테이블 사용.
	goose.SetTableName("v2_goose_db_version")
	if err := goose.Up(db, Dir); err != nil {
		return fmt.Errorf("v2 migrate: up: %w", err)
	}
	return nil
}
