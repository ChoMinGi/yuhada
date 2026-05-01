package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mingicho/yuhada/internal/db/dbgen"
	"github.com/mingicho/yuhada/internal/util"
)

type MemberService struct {
	db *sql.DB
	q  *dbgen.Queries
}

// Sort 옵션 — SearchMembers 결과를 in-memory 정렬.
type SortKey string

const (
	SortDefault     SortKey = "default"
	SortBalanceDesc SortKey = "balance_desc"
	SortBalanceAsc  SortKey = "balance_asc"
)

// CreateMemberInput — 신규 회원 등록.
type CreateMemberInput struct {
	Name          string
	Phone         string // 정규화 된 digits-only
	CardUUID      string // 빈 문자열이면 null
	InitialCharge int64  // 0이면 충전 없음
	Memo          string
	CreatedBy     string // admin UUID (초기 충전 기록용)
}

// Create — 회원 insert + (옵션) 초기 충전.
func (s *MemberService) Create(ctx context.Context, in CreateMemberInput) (dbgen.Member, error) {
	phone := util.NormalizePhone(in.Phone)
	if phone == "" {
		return dbgen.Member{}, fmt.Errorf("%w: phone required", ErrInvalidInput)
	}
	if strings.TrimSpace(in.Name) == "" {
		return dbgen.Member{}, fmt.Errorf("%w: name required", ErrInvalidInput)
	}

	params := dbgen.CreateMemberParams{
		ID:       util.NewID(),
		Name:     strings.TrimSpace(in.Name),
		Phone:    phone,
		CardUuid: nullStr(in.CardUUID),
		Balance:  0,
		Memo:     nullStr(in.Memo),
	}
	m, err := s.q.CreateMember(ctx, params)
	if err != nil {
		return dbgen.Member{}, mapInsertErr(err)
	}

	// 초기 충전이 있으면 wallet 경로로 처리 (감사 로그 + 트랜잭션 보장)
	// wallet은 순환 import 피하려고 여기서 직접 호출 X.
	// 호출 쪽(핸들러)이 Create → Charge 순서로 부르는 게 깔끔. 여기선 INSERT만.
	return m, nil
}

// Get — 단건 조회.
func (s *MemberService) Get(ctx context.Context, id string) (dbgen.Member, error) {
	m, err := s.q.GetMember(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.Member{}, ErrNotFound
	}
	return m, err
}

// GetByCardUUID — 고객 조회 페이지 (/c/{uuid}) 에서 사용. is_active=1만.
func (s *MemberService) GetByCardUUID(ctx context.Context, cardUUID string) (dbgen.Member, error) {
	m, err := s.q.GetMemberByCardUUID(ctx, nullStr(cardUUID))
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.Member{}, ErrNotFound
	}
	return m, err
}

// Search — 회원 검색 + 정렬.
func (s *MemberService) Search(ctx context.Context, q string, sortKey SortKey) ([]dbgen.Member, error) {
	// LIKE 패턴 래핑 ('q' → '%q%', 빈 문자열 → '%')
	pattern := "%" + q + "%"
	members, err := s.q.SearchMembers(ctx, pattern)
	if err != nil {
		return nil, err
	}
	applySort(members, sortKey)
	return members, nil
}

// UpdateMemo — 메모만 수정.
func (s *MemberService) UpdateMemo(ctx context.Context, id, memo string) error {
	return s.q.UpdateMemberMemo(ctx, dbgen.UpdateMemberMemoParams{
		Memo: nullStr(memo),
		ID:   id,
	})
}

// IssueCard — 빈 카드 UUID 지정.
func (s *MemberService) IssueCard(ctx context.Context, id, cardUUID string) error {
	if strings.TrimSpace(cardUUID) == "" {
		cardUUID = util.NewCardUUID()
	}
	return s.q.UpdateMemberCard(ctx, dbgen.UpdateMemberCardParams{
		CardUuid: nullStr(cardUUID),
		ID:       id,
	})
}

// ReportLost — 분실 신고. 즉시 is_active=0 (충전/차감 차단).
func (s *MemberService) ReportLost(ctx context.Context, id string) error {
	return s.q.DeactivateMember(ctx, id)
}

// Reactivate — 재활성화.
func (s *MemberService) Reactivate(ctx context.Context, id string) error {
	return s.q.ReactivateMember(ctx, id)
}

// LinkKakao — 카카오 user id 연결 (본인 인증 후).
func (s *MemberService) LinkKakao(ctx context.Context, id, kakaoUserID string) error {
	return s.q.LinkKakaoUser(ctx, dbgen.LinkKakaoUserParams{
		KakaoUserID: nullStr(kakaoUserID),
		ID:          id,
	})
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func nullStr(s string) sql.NullString {
	if strings.TrimSpace(s) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func applySort(members []dbgen.Member, key SortKey) {
	switch key {
	case SortBalanceDesc:
		sort.SliceStable(members, func(i, j int) bool {
			return members[i].Balance > members[j].Balance
		})
	case SortBalanceAsc:
		sort.SliceStable(members, func(i, j int) bool {
			return members[i].Balance < members[j].Balance
		})
	}
}

// mapInsertErr — SQLite 제약 에러를 서비스 에러로.
func mapInsertErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "members.phone"):
		return ErrDuplicatePhone
	case strings.Contains(msg, "members.card_uuid"):
		return ErrDuplicateCard
	}
	return err
}
