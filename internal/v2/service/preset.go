package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
)

// PresetService — deduct_amount_presets + service_memo_presets CRUD.
// 설정 하위 메뉴 "차감 금액", "시술 메모" 관리용.
type PresetService struct {
	db *sql.DB
	q  *dbgen.Queries
}

// ─────────────────────────────────────────────
// deduct_amount_presets
// ─────────────────────────────────────────────

// ListActiveDeductAmounts — 활성 프리셋만 (차감 모달용).
func (s *PresetService) ListActiveDeductAmounts(ctx context.Context) ([]dbgen.DeductAmountPreset, error) {
	return s.q.ListActiveDeductPresets(ctx)
}

// ListAllDeductAmounts — 관리 화면용 (비활성 포함).
func (s *PresetService) ListAllDeductAmounts(ctx context.Context) ([]dbgen.DeductAmountPreset, error) {
	return s.q.ListAllDeductPresets(ctx)
}

// CreateDeductAmount — 신규 등록. id 는 서버 발급.
func (s *PresetService) CreateDeductAmount(ctx context.Context, amount int64, displayOrder int64) error {
	if amount <= 0 {
		return fmt.Errorf("%w: amount must be > 0", ErrInvalidInput)
	}
	_, err := s.q.CreateDeductPreset(ctx, dbgen.CreateDeductPresetParams{
		ID:           util.NewID(),
		Amount:       amount,
		DisplayOrder: displayOrder,
	})
	return err
}

// UpdateDeductAmount — 기존 항목 수정.
func (s *PresetService) UpdateDeductAmount(ctx context.Context, id string, amount int64, displayOrder int64) error {
	if amount <= 0 {
		return fmt.Errorf("%w: amount must be > 0", ErrInvalidInput)
	}
	return s.q.UpdateDeductPreset(ctx, dbgen.UpdateDeductPresetParams{
		ID:           id,
		Amount:       amount,
		DisplayOrder: displayOrder,
	})
}

// DeactivateDeductAmount — is_active=0.
func (s *PresetService) DeactivateDeductAmount(ctx context.Context, id string) error {
	return s.q.DeactivateDeductPreset(ctx, id)
}

// ReactivateDeductAmount — is_active=1.
func (s *PresetService) ReactivateDeductAmount(ctx context.Context, id string) error {
	return s.q.ReactivateDeductPreset(ctx, id)
}

// DeleteDeductAmount — 하드 삭제. 외래키 참조 없음.
func (s *PresetService) DeleteDeductAmount(ctx context.Context, id string) error {
	return s.q.DeleteDeductPreset(ctx, id)
}

// ReorderDeductAmounts — 드래그 앤 드롭 순서 변경. orderedIDs 순서대로 display_order 를 10, 20, 30... 재할당.
// 한 트랜잭션 안에서 모두 처리해 부분 실패로 순서가 깨지지 않게 보장.
func (s *PresetService) ReorderDeductAmounts(ctx context.Context, orderedIDs []string) error {
	if len(orderedIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	q := dbgen.New(tx)
	for i, id := range orderedIDs {
		if err := q.UpdateDeductPresetOrder(ctx, dbgen.UpdateDeductPresetOrderParams{
			DisplayOrder: int64((i + 1) * 10),
			ID:           id,
		}); err != nil {
			return fmt.Errorf("update order %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

// ─────────────────────────────────────────────
// service_memo_presets
// ─────────────────────────────────────────────

// ListActiveServiceMemos — 활성 프리셋만 (차감 모달용).
func (s *PresetService) ListActiveServiceMemos(ctx context.Context) ([]dbgen.ServiceMemoPreset, error) {
	return s.q.ListActiveServiceMemoPresets(ctx)
}

// ListAllServiceMemos — 관리 화면용 (비활성 포함).
func (s *PresetService) ListAllServiceMemos(ctx context.Context) ([]dbgen.ServiceMemoPreset, error) {
	return s.q.ListAllServiceMemoPresets(ctx)
}

// CreateServiceMemo — 신규 등록. id 는 서버 발급.
func (s *PresetService) CreateServiceMemo(ctx context.Context, label string, displayOrder int64) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("%w: label required", ErrInvalidInput)
	}
	_, err := s.q.CreateServiceMemoPreset(ctx, dbgen.CreateServiceMemoPresetParams{
		ID:           util.NewID(),
		Label:        label,
		DisplayOrder: displayOrder,
	})
	return err
}

// UpdateServiceMemo — 기존 항목 수정.
func (s *PresetService) UpdateServiceMemo(ctx context.Context, id string, label string, displayOrder int64) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("%w: label required", ErrInvalidInput)
	}
	return s.q.UpdateServiceMemoPreset(ctx, dbgen.UpdateServiceMemoPresetParams{
		ID:           id,
		Label:        label,
		DisplayOrder: displayOrder,
	})
}

// DeactivateServiceMemo — is_active=0.
func (s *PresetService) DeactivateServiceMemo(ctx context.Context, id string) error {
	return s.q.DeactivateServiceMemoPreset(ctx, id)
}

// ReactivateServiceMemo — is_active=1.
func (s *PresetService) ReactivateServiceMemo(ctx context.Context, id string) error {
	return s.q.ReactivateServiceMemoPreset(ctx, id)
}

// DeleteServiceMemo — 하드 삭제. 외래키 참조 없음.
func (s *PresetService) DeleteServiceMemo(ctx context.Context, id string) error {
	return s.q.DeleteServiceMemoPreset(ctx, id)
}

// ReorderServiceMemos — 드래그 앤 드롭 순서 변경. 트랜잭션 안에서 display_order 를 10, 20, 30... 재할당.
func (s *PresetService) ReorderServiceMemos(ctx context.Context, orderedIDs []string) error {
	if len(orderedIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	q := dbgen.New(tx)
	for i, id := range orderedIDs {
		if err := q.UpdateServiceMemoPresetOrder(ctx, dbgen.UpdateServiceMemoPresetOrderParams{
			DisplayOrder: int64((i + 1) * 10),
			ID:           id,
		}); err != nil {
			return fmt.Errorf("update order %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

