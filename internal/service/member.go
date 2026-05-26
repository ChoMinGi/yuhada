package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/mingicho/yuhada/internal/crypto"
	"github.com/mingicho/yuhada/internal/db/dbgen"
	"github.com/mingicho/yuhada/internal/util"
)

type MemberService struct {
	db  *sql.DB
	q   *dbgen.Queries
	enc *crypto.Enc
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
		Name:     s.enc.Encrypt(strings.TrimSpace(in.Name)),
		Phone:    s.enc.EncryptDeterministic(phone),
		CardUuid: nullStr(in.CardUUID),
		Balance:  0,
		Memo:     encryptNullStr(s.enc, in.Memo),
	}
	m, err := s.q.CreateMember(ctx, params)
	if err != nil {
		return dbgen.Member{}, mapInsertErr(err)
	}

	s.decryptMember(&m)
	return m, nil
}

// Get — 단건 조회.
func (s *MemberService) Get(ctx context.Context, id string) (dbgen.Member, error) {
	m, err := s.q.GetMember(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.Member{}, ErrNotFound
	}
	if err != nil {
		return dbgen.Member{}, err
	}
	s.decryptMember(&m)
	return m, err
}

// GetByCardUUID — 고객 조회 페이지 (/c/{uuid}) 에서 사용. is_active=1만.
func (s *MemberService) GetByCardUUID(ctx context.Context, cardUUID string) (dbgen.Member, error) {
	m, err := s.q.GetMemberByCardUUID(ctx, nullStr(cardUUID))
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.Member{}, ErrNotFound
	}
	if err != nil {
		return dbgen.Member{}, err
	}
	s.decryptMember(&m)
	return m, err
}

// Search — 회원 검색 + 정렬.
// 암호화 활성 시 전체 조회 후 Go에서 필터링.
func (s *MemberService) Search(ctx context.Context, q string, sortKey SortKey) ([]dbgen.Member, error) {
	var members []dbgen.Member
	var err error

	if s.enc != nil {
		// 암호화 모드: 전체 조회 → 복호화 → 필터
		members, err = s.q.ListAllMembers(ctx)
		if err != nil {
			return nil, err
		}
		for i := range members {
			s.decryptMember(&members[i])
		}
		if q = strings.TrimSpace(q); q != "" {
			members = filterMembers(members, q)
		}
	} else {
		pattern := "%" + q + "%"
		members, err = s.q.SearchMembers(ctx, pattern)
		if err != nil {
			return nil, err
		}
	}

	applySort(members, sortKey)
	return members, nil
}

// UpdateMemo — 메모만 수정.
func (s *MemberService) UpdateMemo(ctx context.Context, id, memo string) error {
	return s.q.UpdateMemberMemo(ctx, dbgen.UpdateMemberMemoParams{
		Memo: encryptNullStr(s.enc, memo),
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

// Delete — 회원 + 거래 내역 삭제.
func (s *MemberService) Delete(ctx context.Context, id string) error {
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
	if err := q.DeleteTransactionsByMember(ctx, id); err != nil {
		return fmt.Errorf("delete transactions: %w", err)
	}
	if err := q.DeleteMember(ctx, id); err != nil {
		return fmt.Errorf("delete member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

// LinkKakao — 카카오 user id 연결 (본인 인증 후).
func (s *MemberService) LinkKakao(ctx context.Context, id, kakaoUserID string) error {
	return s.q.LinkKakaoUser(ctx, dbgen.LinkKakaoUserParams{
		KakaoUserID: nullStr(kakaoUserID),
		ID:          id,
	})
}

// MigrateEncryption — 기존 평문 레코드를 암호화. 서버 시작 시 1회 호출.
func (s *MemberService) MigrateEncryption(ctx context.Context) error {
	if s.enc == nil {
		return nil
	}
	members, err := s.q.ListAllMembers(ctx)
	if err != nil {
		return err
	}
	migrated := 0
	for _, m := range members {
		if crypto.IsEncrypted(m.Name) {
			continue // 이미 암호화됨
		}
		encName := s.enc.Encrypt(m.Name)
		encPhone := s.enc.EncryptDeterministic(m.Phone)
		var encMemo sql.NullString
		if m.Memo.Valid {
			encMemo = sql.NullString{String: s.enc.Encrypt(m.Memo.String), Valid: true}
		}
		_, err := s.db.ExecContext(ctx,
			"UPDATE members SET name = ?, phone = ?, memo = ? WHERE id = ?",
			encName, encPhone, encMemo, m.ID)
		if err != nil {
			return fmt.Errorf("migrate member %s: %w", m.ID, err)
		}
		migrated++
	}
	if migrated > 0 {
		slog.Info("encryption migration", "migrated", migrated)
	}
	return nil
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func (s *MemberService) decryptMember(m *dbgen.Member) {
	if s.enc == nil {
		return
	}
	m.Name = s.enc.Decrypt(m.Name)
	m.Phone = s.enc.Decrypt(m.Phone)
	if m.Memo.Valid {
		m.Memo.String = s.enc.Decrypt(m.Memo.String)
	}
}

func filterMembers(members []dbgen.Member, q string) []dbgen.Member {
	q = strings.ToLower(q)
	out := members[:0]
	for _, m := range members {
		if strings.Contains(strings.ToLower(m.Name), q) ||
			strings.Contains(m.Phone, q) {
			out = append(out, m)
		}
	}
	return out
}

func encryptNullStr(enc *crypto.Enc, s string) sql.NullString {
	s = strings.TrimSpace(s)
	if s == "" {
		return sql.NullString{}
	}
	if enc != nil {
		s = enc.Encrypt(s)
	}
	return sql.NullString{String: s, Valid: true}
}

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
