// Package analytics defines the read-only member population used by business
// dashboards. It never changes operational membership, balances or history.
package analytics

import "strings"

type Scope struct {
	excludedIDs []string
}

func New(excludedMemberIDs ...string) Scope {
	ids := make([]string, 0, len(excludedMemberIDs))
	seen := map[string]bool{}
	for _, id := range excludedMemberIDs {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return Scope{excludedIDs: ids}
}

func (s Scope) ExcludedMemberIDs() []string {
	return append([]string{}, s.excludedIDs...)
}

// Clause appends a parameterized exclusion to an existing WHERE predicate.
// Only code-owned known member columns are accepted; IDs are never SQL text.
func (s Scope) Clause(column string) (string, []any) {
	switch column {
	case "id", "member_id", "t.member_id", "m.id", "mb.id", "m.member_id", "ms.member_id":
	default:
		panic("analytics: unsupported member column")
	}
	if len(s.excludedIDs) == 0 {
		return "", nil
	}
	args := make([]any, len(s.excludedIDs))
	for i, id := range s.excludedIDs {
		args[i] = id
	}
	return " AND " + column + " NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + ")", args
}
