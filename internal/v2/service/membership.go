package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
)

// MembershipService — memberships 조회 (판매 이력, 회원 상세 표시용).
type MembershipService struct {
	db *sql.DB
	q  *dbgen.Queries
}

// ListByMember — 특정 회원의 정액권 구매 이력 (최신순).
// 회원 상세 MEMBERSHIPS 섹션 데이터.
func (s *MembershipService) ListByMember(ctx context.Context, memberID string) ([]dbgen.Membership, error) {
	return s.q.ListMembershipsByMember(ctx, memberID)
}

// ListChargeHistoryWithPlan — R3 LEFT JOIN.
// 회원 상세 최근 거래 섹션에서 charge 로우에 등급 뱃지를 붙이기 위해 사용.
// MembershipID.Valid == false 인 로우는 일반 충전.
func (s *MembershipService) ListChargeHistoryWithPlan(ctx context.Context, memberID string) ([]dbgen.ListChargeHistoryWithPlanRow, error) {
	return s.q.ListChargeHistoryWithPlan(ctx, memberID)
}

// Get — 단건 조회.
func (s *MembershipService) Get(ctx context.Context, id string) (dbgen.Membership, error) {
	m, err := s.q.GetMembership(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.Membership{}, ErrNotFound
	}
	return m, err
}
