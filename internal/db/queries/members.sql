-- name: CreateMember :one
INSERT INTO members (id, name, phone, card_uuid, balance, memo)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMember :one
SELECT * FROM members WHERE id = ? LIMIT 1;

-- name: GetMemberByPhone :one
SELECT * FROM members WHERE phone = ? LIMIT 1;

-- name: GetMemberByCardUUID :one
SELECT * FROM members
WHERE card_uuid = ?
  AND is_active = 1
LIMIT 1;

-- name: GetMemberByKakaoUserID :one
SELECT * FROM members WHERE kakao_user_id = ? LIMIT 1;

-- name: SearchMembers :many
SELECT * FROM members
WHERE (name LIKE ?1 OR phone LIKE ?1)
ORDER BY created_at DESC
LIMIT 500;

-- name: ListAllMembers :many
SELECT * FROM members ORDER BY created_at DESC LIMIT 500;

-- name: UpdateMemberMemo :exec
UPDATE members SET memo = ? WHERE id = ?;

-- name: UpdateMemberCard :exec
UPDATE members SET card_uuid = ? WHERE id = ?;

-- name: DeactivateMember :exec
UPDATE members SET is_active = 0 WHERE id = ?;

-- name: ReactivateMember :exec
UPDATE members SET is_active = 1 WHERE id = ?;

-- name: LinkKakaoUser :exec
UPDATE members SET kakao_user_id = ? WHERE id = ?;

-- name: AddToBalance :exec
UPDATE members SET balance = balance + ? WHERE id = ?;

-- name: CountActiveMembers :one
SELECT COUNT(*) FROM members WHERE is_active = 1;

-- name: CountCardsIssued :one
SELECT COUNT(*) FROM members WHERE card_uuid IS NOT NULL;

-- name: CountCardsLost :one
SELECT COUNT(*) FROM members WHERE card_uuid IS NOT NULL AND is_active = 0;

-- name: DeleteMember :exec
DELETE FROM members WHERE id = ?;

-- name: DeleteTransactionsByMember :exec
DELETE FROM transactions WHERE member_id = ?;
