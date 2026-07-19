package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mingicho/yuhada/internal/crypto"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
)

// MemberService — v2 회원 CRUD. v1 MemberService 를 v2 dbgen 으로 재구현.
// 암호화 정책은 v1 과 동일 (같은 ENCRYPTION_KEY 공유).
type MemberService struct {
	db  *sql.DB
	q   *dbgen.Queries
	enc *crypto.Enc
}

// SortKey — Search 결과 in-memory 정렬 옵션.
type SortKey string

const (
	SortDefault     SortKey = "default"
	SortBalanceDesc SortKey = "balance_desc"
	SortBalanceAsc  SortKey = "balance_asc"
)

// CreateMemberInput — 신규 회원 등록. v2는 초기 충전 로직을 여기서 다루지 않음
// (핸들러에서 별도로 charge 호출).
type CreateMemberInput struct {
	Name     string
	Phone    string
	CardUUID string
	Memo     string
}

// Create — 회원 insert. PII 는 v1 crypto 로 암호화.
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

// Get — 단건 조회 + 복호화.
func (s *MemberService) Get(ctx context.Context, id string) (dbgen.Member, error) {
	m, err := s.q.GetMember(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.Member{}, ErrNotFound
	}
	if err != nil {
		return dbgen.Member{}, err
	}
	s.decryptMember(&m)
	return m, nil
}

// Search — 회원 검색 + 정렬. 암호화 활성 시 전체 조회 후 in-memory 필터.
func (s *MemberService) Search(ctx context.Context, q string, sortKey SortKey) ([]dbgen.Member, error) {
	var members []dbgen.Member
	var err error

	if s.enc != nil {
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
		pattern := "%" + strings.TrimSpace(q) + "%"
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

// Delete — 회원 + 거래 내역 삭제. 트랜잭션.
// memberships 는 charge_tx_id 로 transactions 를 참조하므로 순서 주의:
// memberships → transactions → members 순으로 삭제해야 FK RESTRICT 위반 없음.
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

	// memberships 먼저 (charge_tx_id → transactions FK).
	if _, err := tx.ExecContext(ctx, `DELETE FROM memberships WHERE member_id = ?`, id); err != nil {
		return fmt.Errorf("delete memberships: %w", err)
	}
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

// ─────────────────────────────────────────────
// helpers (v1 service.member 과 동일 패턴)
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
