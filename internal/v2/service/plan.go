package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
)

// PlanService — membership_plans CRUD.
type PlanService struct {
	db *sql.DB
	q  *dbgen.Queries
}

// CreatePlanInput — 신규 가격표 항목 등록.
type CreatePlanInput struct {
	TierLabel     string
	TierAmount    int64
	PaymentMethod string // "cash" | "card"
	Price         int64
	CreditAmount  int64
	PerkLabel     string
	DisplayOrder  int64
}

// UpdatePlanInput — 기존 항목 수정. ID 는 별도 인자.
type UpdatePlanInput = CreatePlanInput

// ListActive — 활성 플랜만 (충전 모달용).
func (s *PlanService) ListActive(ctx context.Context) ([]dbgen.V2MembershipPlan, error) {
	return s.q.ListActivePlans(ctx)
}

// ListAll — 관리 화면용 (비활성 포함).
func (s *PlanService) ListAll(ctx context.Context) ([]dbgen.V2MembershipPlan, error) {
	return s.q.ListAllPlans(ctx)
}

// Get — 단건 조회.
func (s *PlanService) Get(ctx context.Context, id string) (dbgen.V2MembershipPlan, error) {
	p, err := s.q.GetPlan(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.V2MembershipPlan{}, ErrNotFound
	}
	return p, err
}

// Create — 신규 등록. id 는 UUID 로 서버 발급.
func (s *PlanService) Create(ctx context.Context, in CreatePlanInput) (dbgen.V2MembershipPlan, error) {
	if err := validatePlan(in); err != nil {
		return dbgen.V2MembershipPlan{}, err
	}
	return s.q.CreatePlan(ctx, dbgen.CreatePlanParams{
		ID:            util.NewID(),
		TierLabel:     strings.TrimSpace(in.TierLabel),
		TierAmount:    in.TierAmount,
		PaymentMethod: in.PaymentMethod,
		Price:         in.Price,
		CreditAmount:  in.CreditAmount,
		PerkLabel:     nullTrimmed(in.PerkLabel),
		DisplayOrder:  in.DisplayOrder,
	})
}

// Update — 기존 항목 수정 (id 기반).
func (s *PlanService) Update(ctx context.Context, id string, in UpdatePlanInput) error {
	if err := validatePlan(in); err != nil {
		return err
	}
	return s.q.UpdatePlan(ctx, dbgen.UpdatePlanParams{
		ID:            id,
		TierLabel:     strings.TrimSpace(in.TierLabel),
		TierAmount:    in.TierAmount,
		PaymentMethod: in.PaymentMethod,
		Price:         in.Price,
		CreditAmount:  in.CreditAmount,
		PerkLabel:     nullTrimmed(in.PerkLabel),
		DisplayOrder:  in.DisplayOrder,
	})
}

// Deactivate — is_active=0.
func (s *PlanService) Deactivate(ctx context.Context, id string) error {
	return s.q.DeactivatePlan(ctx, id)
}

// Reactivate — is_active=1.
func (s *PlanService) Reactivate(ctx context.Context, id string) error {
	return s.q.ReactivatePlan(ctx, id)
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func validatePlan(in CreatePlanInput) error {
	if strings.TrimSpace(in.TierLabel) == "" {
		return fmt.Errorf("%w: tier_label required", ErrInvalidInput)
	}
	if in.TierAmount <= 0 {
		return fmt.Errorf("%w: tier_amount must be > 0", ErrInvalidInput)
	}
	if in.PaymentMethod != "cash" && in.PaymentMethod != "card" {
		return fmt.Errorf("%w: payment_method must be cash|card", ErrInvalidInput)
	}
	if in.Price <= 0 || in.CreditAmount <= 0 {
		return fmt.Errorf("%w: price and credit_amount must be > 0", ErrInvalidInput)
	}
	return nil
}

func nullTrimmed(s string) sql.NullString {
	s = strings.TrimSpace(s)
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
