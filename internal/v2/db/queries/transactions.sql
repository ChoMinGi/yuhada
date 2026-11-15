-- Cloned from v1 internal/db/queries/transactions.sql (v2-isolated dbgen).

-- name: InsertTransaction :one
INSERT INTO transactions (id, member_id, type, amount, balance_after, memo, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListTransactionsByMember :many
SELECT * FROM transactions
WHERE member_id = ?
ORDER BY created_at DESC, id DESC
LIMIT 50;

-- name: ListRecentTransactions :many
SELECT
  t.id            AS tx_id,
  t.type          AS tx_type,
  t.amount        AS tx_amount,
  t.memo          AS tx_memo,
  t.balance_after AS tx_balance_after,
  t.created_at    AS tx_created_at,
  m.id            AS member_id,
  m.name          AS member_name
FROM transactions t
JOIN members m ON m.id = t.member_id
ORDER BY t.created_at DESC, t.id DESC
LIMIT 20;

-- name: ListTransactionsInPeriod :many
SELECT
  t.id            AS tx_id,
  t.type          AS tx_type,
  t.amount        AS tx_amount,
  t.memo          AS tx_memo,
  t.balance_after AS tx_balance_after,
  t.created_at    AS tx_created_at,
  m.id            AS member_id,
  m.name          AS member_name
FROM transactions t
JOIN members m ON m.id = t.member_id
WHERE t.created_at >= ?
  AND t.created_at <  ?
ORDER BY t.created_at DESC, t.id DESC
LIMIT 500;

-- name: DeleteTransactionsByMember :exec
DELETE FROM transactions WHERE member_id = ?;
