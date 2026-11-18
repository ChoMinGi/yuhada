package analytics

import (
	"database/sql"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func TestScopeNormalizesCopiesAndDoesNotTurnIDsIntoSQL(t *testing.T) {
	malicious := "x') OR 1=1 --"
	input := []string{" owner ", "", "owner", malicious, "  "}
	scope := New(input...)
	input[0] = "customer"
	want := []string{"owner", malicious}
	if !reflect.DeepEqual(scope.ExcludedMemberIDs(), want) {
		t.Fatalf("normalization or caller slice alias: %v", scope.ExcludedMemberIDs())
	}
	copy := scope.ExcludedMemberIDs()
	copy[0] = "customer"
	if !reflect.DeepEqual(scope.ExcludedMemberIDs(), want) {
		t.Fatal("returned IDs changed the immutable scope")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE members(id TEXT PRIMARY KEY); INSERT INTO members(id) VALUES('owner'),('customer')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO members(id) VALUES(?)`, malicious); err != nil {
		t.Fatal(err)
	}
	clause, args := scope.Clause("id")
	var remaining int
	if err = db.QueryRow("SELECT COUNT(*) FROM members WHERE 1=1"+clause, args...).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("IDs must be bound values, remaining=%d err=%v", remaining, err)
	}
	emptyClause, emptyArgs := New("", " ").Clause("id")
	if emptyClause != "" || len(emptyArgs) != 0 {
		t.Fatal("empty scope changed population")
	}
}

func TestScopeRejectsCallerControlledColumnSQL(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unrecognized SQL column was accepted")
		}
	}()
	New("owner").Clause("id) OR 1=1 --")
}
