// Package seed — v2 첫 부팅 시 더미 회원 보강.
//
// plans 6종은 마이그레이션 00002_seed_plans.sql 에서 이미 idempotent 하게 채워짐.
// 여기서는 매장에서 실제 사용하기 전 테스트용으로 회원 몇 명만 넣는다.
//
// idempotent 보장: INSERT OR IGNORE 사용. 매 부팅마다 호출되어도 안전.
package seed

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/service"
)

// EnsureSeeded — 더미 회원이 없으면 삽입. v2 서비스의 crypto 를 그대로 사용해서
// v1 과 동일한 암호화 정책 적용.
func EnsureSeeded(ctx context.Context, database *sql.DB, s *service.Services) error {
	// 회원 수 확인 (이미 있으면 스킵).
	var count int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM members`).Scan(&count); err != nil {
		return fmt.Errorf("count members: %w", err)
	}
	if count > 0 {
		slog.Info("v2 seed skipped", "existing_members", count)
		return nil
	}

	dummies := []service.CreateMemberInput{
		{Name: "테스트1", Phone: "01000000001", Memo: "v2 시드 회원 (자동 생성)"},
		{Name: "테스트2", Phone: "01000000002", Memo: "v2 시드 회원 (자동 생성)"},
		{Name: "테스트3", Phone: "01000000003", Memo: "v2 시드 회원 (자동 생성)"},
	}

	inserted := 0
	for _, in := range dummies {
		if _, err := s.Member.Create(ctx, in); err != nil {
			// 중복이면 무시하고 계속.
			slog.Warn("v2 seed member create failed", "phone_tail", tail4(in.Phone), "err", err)
			continue
		}
		inserted++
	}
	slog.Info("v2 seed done", "members_inserted", inserted, "seed_id", util.NewID()[:8])
	return nil
}

func tail4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}
