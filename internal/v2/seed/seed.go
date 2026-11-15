// Package seed contains the deprecated isolated demo-database seeder.
// It must never seed a database containing the operational canonical ledger.
package seed

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/mingicho/yuhada/internal/crypto"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/service"
)

// EnsureSeeded inserts test-only members into an isolated legacy v2 database.
// Deprecated: the shared-ledger application must not call demo seeding.
func EnsureSeeded(ctx context.Context, database *sql.DB, s *service.Services) error {
	if database == nil {
		return fmt.Errorf("v2 seed: database is required")
	}
	var canonical int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table','view') AND lower(name) IN ('members','transactions')`).Scan(&canonical); err != nil {
		return fmt.Errorf("v2 seed: inspect schema: %w", err)
	}
	if canonical > 0 {
		return fmt.Errorf("v2 seed: refusing demo seeding on a canonical ledger database")
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("v2 seed: begin: %w", err)
	}
	defer tx.Rollback()
	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM v2_members`).Scan(&count); err != nil {
		return fmt.Errorf("v2 seed: count demo members: %w", err)
	}
	if count > 0 {
		slog.Info("v2 demo seed skipped", "existing_members", count)
		return nil
	}
	var enc *crypto.Enc
	if s != nil {
		enc = s.Enc
	}
	dummies := []struct{ name, phone string }{{"테스트1", "01000000001"}, {"테스트2", "01000000002"}, {"테스트3", "01000000003"}}
	for _, in := range dummies {
		// Deliberately target only demo tables: MemberService now writes canonical members.
		if _, err := tx.ExecContext(ctx, `INSERT INTO v2_members (id,name,phone,balance,memo) VALUES (?,?,?,0,?)`, util.NewID(), enc.Encrypt(in.name), enc.EncryptDeterministic(in.phone), enc.Encrypt("v2 시드 회원 (자동 생성)")); err != nil {
			return fmt.Errorf("v2 seed: insert demo member: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("v2 seed: commit: %w", err)
	}
	slog.Info("v2 demo seed done", "members_inserted", len(dummies))
	return nil
}
