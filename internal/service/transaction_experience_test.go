package service

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestTransactionSearchPopulationAndTotals(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%t", encrypted), func(t *testing.T) {
			var s *Services
			if encrypted {
				s = testServicesWithEnc(t)
			} else {
				s = testServices(t)
			}
			ctx := context.Background()
			a, err := s.Member.Create(ctx, CreateMemberInput{Name: "Alpha 고객", Phone: "01000000001"})
			if err != nil {
				t.Fatal(err)
			}
			b, err := s.Member.Create(ctx, CreateMemberInput{Name: "Beta 고객", Phone: "01000000002"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Wallet.Charge(ctx, a.ID, 50000, "A 충전", "audit"); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Wallet.Deduct(ctx, a.ID, 10000, "A 시술", "audit"); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Wallet.Charge(ctx, b.ID, 90000, "B 충전", "audit"); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				q, kind        string
				rows           int
				deduct, charge int64
			}{{"Alpha", "", 2, 10000, 50000}, {"Alpha", "deduct", 1, 10000, 50000}, {"00000001", "charge", 1, 10000, 50000}, {"NO_SUCH_MEMBER_987654", "", 0, 0, 0}, {"NO_SUCH_MEMBER_987654", "charge", 0, 0, 0}, {"  ", "", 3, 10000, 140000}} {
				filter := TxFilter{Period: PeriodAll, Type: tc.kind, Q: tc.q}
				rows, err := s.Tx.ListInPeriod(ctx, filter)
				if err != nil {
					t.Fatal(err)
				}
				deduct, charge, err := s.Tx.SumsInPeriod(ctx, filter)
				if err != nil || len(rows) != tc.rows || deduct != tc.deduct || charge != tc.charge {
					t.Fatalf("filter %+v: rows=%d deduct=%d charge=%d err=%v", filter, len(rows), deduct, charge, err)
				}
				for _, row := range rows {
					if tc.q != "  " && row.MemberID != a.ID {
						t.Fatal("search exposed an unrelated customer's transaction")
					}
				}
			}
		})
	}
}

func TestTransactionPagesReachAllHistoryWithStableOrderAndSameFilteredCount(t *testing.T) {
	s := testServicesWithEnc(t)
	s.DB.SetMaxOpenConns(1)
	ctx := context.Background()
	member, err := s.Member.Create(ctx, CreateMemberInput{Name: "전체 이력 고객", Phone: "01087654321"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 505; i++ {
		if _, err = s.DB.Exec(`INSERT INTO transactions(id,member_id,type,amount,balance_after,created_at) VALUES(?,?,'charge',1,?,'2026-01-01T00:00:00.000Z')`, fmt.Sprintf("page-%04d", i), member.ID, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.DB.Exec("UPDATE members SET balance=505 WHERE id=?", member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	filter := TxFilter{Period: PeriodAll, Q: "전체 이력"}
	seen := map[string]bool{}
	for offset := 0; offset < 505; offset += 20 {
		rows, total, err := s.Tx.ListInPeriodPage(ctx, filter, 20, offset)
		wantRows := 20
		if offset == 500 {
			wantRows = 5
		}
		if err != nil || total != 505 || len(rows) != wantRows {
			t.Fatalf("offset=%d rows=%d total=%d err=%v", offset, len(rows), total, err)
		}
		for i, row := range rows {
			if seen[row.TxID] || row.TxID != fmt.Sprintf("page-%04d", 504-offset-i) || row.MemberName != "전체 이력 고객" {
				t.Fatalf("non-deterministic/decrypted page at%d: %+v", offset, row)
			}
			seen[row.TxID] = true
		}
	}
	if len(seen) != 505 {
		t.Fatal("page boundary silently dropped old transactions")
	}
	for _, filter := range []TxFilter{{Period: PeriodAll, Q: "없는고객"}, {Period: PeriodAll, Q: "전체 이력", Type: "deduct"}} {
		rows, total, err := s.Tx.ListInPeriodPage(ctx, filter, 20, 0)
		if err != nil || total != 0 || len(rows) != 0 {
			t.Fatalf("empty page/count mismatch: %d %d %v", len(rows), total, err)
		}
	}
	rows, total, err := s.Tx.ListInPeriodPage(ctx, filter, 99999, -1)
	if err != nil || total != 505 || len(rows) != 20 {
		t.Fatalf("unbounded page accepted: %d %d %v", len(rows), total, err)
	}
	rows, total, err = s.Tx.ListInPeriodPage(ctx, filter, 20, 99999)
	if err != nil || total != 505 || len(rows) != 0 {
		t.Fatalf("out-of-range page changed population: %d %d %v", len(rows), total, err)
	}
}

func TestTransactionEncryptedSearchIncludesMembersBeyondDisplayLimit(t *testing.T) {
	s := testServicesWithEnc(t)
	ctx := context.Background()
	old, err := s.Member.Create(ctx, CreateMemberInput{Name: "먼저 등록한 고객", Phone: "01099999999"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE members SET created_at='2020-01-01T00:00:00.000Z' WHERE id=?", old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Wallet.Charge(ctx, old.ID, 123000, "기존 고객 충전", "audit"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 501; i++ {
		if _, err = s.Member.Create(ctx, CreateMemberInput{Name: fmt.Sprintf("추가 고객 %d", i), Phone: fmt.Sprintf("010%08d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	filter := TxFilter{Period: PeriodAll, Q: "먼저 등록한 고객"}
	rows, err := s.Tx.ListInPeriod(ctx, filter)
	if err != nil || len(rows) != 1 || rows[0].MemberID != old.ID {
		t.Fatalf("older customer's history hidden: rows=%+v err=%v", rows, err)
	}
	deduct, charge, err := s.Tx.SumsInPeriod(ctx, filter)
	if err != nil || deduct != 0 || charge != 123000 {
		t.Fatalf("older customer's totals wrong: %d %d %v", deduct, charge, err)
	}
}

func TestTransactionSearchResolutionErrorsDoNotBroadenPopulation(t *testing.T) {
	s := testServicesWithEnc(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Tx.ListInPeriod(ctx, TxFilter{Period: PeriodAll, Q: "고객"}); err == nil {
		t.Fatal("search resolution failure returned a successful population")
	}
	if _, _, err := s.Tx.SumsInPeriod(ctx, TxFilter{Period: PeriodAll, Q: "고객"}); err == nil {
		t.Fatal("search resolution failure returned successful totals")
	}
}

func TestPeriodRangeUsesKoreanCalendarAcrossUTCDayWeekAndYear(t *testing.T) {
	for _, tc := range []struct {
		at, today, week, month string
	}{
		{"2026-05-25T16:00:00Z", "2026-05-25T15:00:00.000Z", "2026-05-24T15:00:00.000Z", "2026-04-30T15:00:00.000Z"},
		{"2026-11-01T14:59:59Z", "2026-10-31T15:00:00.000Z", "2026-10-25T15:00:00.000Z", "2026-10-31T15:00:00.000Z"},
		{"2026-11-01T15:00:00Z", "2026-11-01T15:00:00.000Z", "2026-11-01T15:00:00.000Z", "2026-10-31T15:00:00.000Z"},
		{"2026-12-31T15:00:00Z", "2026-12-31T15:00:00.000Z", "2026-12-27T15:00:00.000Z", "2026-12-31T15:00:00.000Z"},
		{"2028-02-28T16:00:00Z", "2028-02-28T15:00:00.000Z", "2028-02-27T15:00:00.000Z", "2028-01-31T15:00:00.000Z"},
	} {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		for period, want := range map[Period]string{PeriodToday: tc.today, PeriodWeek: tc.week, PeriodMonth: tc.month, PeriodAll: "2020-01-01T00:00:00.000Z"} {
			for _, input := range []time.Time{at, at.In(time.FixedZone("unrelated server timezone", -7*60*60))} {
				start, end := period.Range(input)
				if start != want || end != at.Add(time.Minute).UTC().Format("2006-01-02T15:04:05.000Z") {
					t.Fatalf("period %s at %s: bounds=[%s,%s), want start=%s", period, input, start, end, want)
				}
			}
		}
	}
}
