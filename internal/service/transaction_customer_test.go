package service

import (
	"fmt"
	"testing"
)

func TestCustomerActivityDoesNotLoseChargesBeyondMixedHistoryLimit(t *testing.T) {
	s := testServices(t)
	m, err := s.Member.Create(t.Context(), CreateMemberInput{Name: "Activity fixture", Phone: "01098765432"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Tx.db.Exec(`INSERT INTO transactions(id,member_id,type,amount,balance_after,created_at) VALUES('old-charge',?,'charge',220000,220000,'2026-01-01T00:00:00.000Z')`, m.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 65; i++ {
		if _, err = s.Tx.db.Exec(`INSERT INTO transactions(id,member_id,type,amount,balance_after,created_at,memo) VALUES(?,?,'deduct',1000,0,'2026-02-01T00:00:00.000Z','커트')`, fmt.Sprintf("visit-%03d", i), m.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Tx.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Tx.LatestCustomerActivity(t.Context(), m.ID)
	if err != nil || !got.HasTransactions || got.LastCharge == nil || got.LastVisit == nil {
		t.Fatalf("activity missing %+v %v", got, err)
	}
	if got.LastCharge.Amount != 220000 || got.LastCharge.CreatedAt != "2026-01-01T00:00:00.000Z" || got.LastVisit.Memo.String != "커트" {
		t.Fatalf("wrong last activity %+v %+v", got.LastCharge, got.LastVisit)
	}
	empty, err := s.Tx.LatestCustomerActivity(t.Context(), "missing-fixture")
	if err != nil || empty.HasTransactions || empty.LastCharge != nil || empty.LastVisit != nil {
		t.Fatalf("empty activity %+v %v", empty, err)
	}
}
