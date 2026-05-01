package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Open — SQLite 연결 풀 생성 + WAL 등 PRAGMA 설정.
//
// dbPath: "./var/yuhada.db" 같은 파일 경로. 디렉토리는 존재해야 함.
//
// SQLite는 write를 단일 커넥션으로만 처리하는 게 안전 → writer pool 크기 1.
// 읽기는 별도 pool로 분리할 수도 있지만, sql.DB에서 MaxOpenConns=1로 단순화
// (쓰기 + 읽기 모두 직렬화; 성능보다 단순성·일관성 우선).
//
// 사장님 1명 + 고객 조회 수준이면 이 구성으로 충분.
func Open(dbPath string) (*sql.DB, error) {
	// 파일 URI + busy_timeout을 driver 레벨에서 지정
	// modernc/sqlite는 _pragma 파라미터로 PRAGMA 미리 설정 가능
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"+
			"&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"+
			"&_pragma=cache_size(-8000)&_pragma=temp_store(MEMORY)",
		filepath.ToSlash(dbPath),
	)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite writer는 단일 커넥션 권장
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	database.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return database, nil
}
