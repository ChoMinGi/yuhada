-- Cloned from v1 internal/db/queries/v2_members.sql (v2-isolated dbgen).
-- Signatures match v1 for behavioural parity.

-- name: CreateMember :one
INSERT INTO v2_members (id, name, phone, card_uuid, balance, memo)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMember :one
SELECT * FROM v2_members WHERE id = ? LIMIT 1;

-- name: GetMemberByPhone :one
SELECT * FROM v2_members WHERE phone = ? LIMIT 1;

-- name: GetMemberByCardUUID :one
SELECT * FROM v2_members
WHERE card_uuid = ?
  AND is_active = 1
LIMIT 1;

-- name: SearchMembers :many
SELECT * FROM v2_members
WHERE (name LIKE ?1 OR phone LIKE ?1)
ORDER BY created_at DESC
LIMIT 500;

-- name: ListAllMembers :many
SELECT * FROM v2_members ORDER BY created_at DESC LIMIT 500;

-- name: UpdateMemberMemo :exec
UPDATE v2_members SET memo = ? WHERE id = ?;

-- name: UpdateMemberCard :exec
UPDATE v2_members SET card_uuid = ? WHERE id = ?;

-- name: AddToBalance :exec
UPDATE v2_members SET balance = balance + ? WHERE id = ?;

-- name: SubFromBalance :exec
UPDATE v2_members SET balance = balance - ? WHERE id = ?;

-- name: DeactivateMember :exec
UPDATE v2_members SET is_active = 0 WHERE id = ?;

-- name: ReactivateMember :exec
UPDATE v2_members SET is_active = 1 WHERE id = ?;

-- name: CountActiveMembers :one
SELECT COUNT(*) FROM v2_members WHERE is_active = 1;

-- name: DeleteMember :exec
DELETE FROM v2_members WHERE id = ?;
