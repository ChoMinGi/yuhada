-- v2 sales dashboard queries.
-- Powers /v2/admin/sales aggregations. SQL fetches rows; Go layer aggregates.
-- LEFT JOIN to memberships so we capture both plan-based and generic charges
-- (plan_snapshot IS NULL means a generic non-plan charge).

-- name: ListSalesInPeriod :many
-- member_id and member_name power ERP KPIs (active / repeat / TOP members).
-- Name is emitted as ciphertext here; service layer decrypts via crypto.Enc.Decrypt.
SELECT
  t.id            AS tx_id,
  t.amount        AS tx_amount,
  t.created_at    AS tx_created_at,
  t.member_id     AS member_id,
  mb.name         AS member_name,
  m.plan_snapshot AS plan_snapshot
FROM transactions t
LEFT JOIN memberships m ON m.charge_tx_id = t.id
LEFT JOIN members mb    ON mb.id           = t.member_id
WHERE t.type = 'charge'
  AND t.created_at >= ?
ORDER BY t.created_at DESC, t.id DESC;

-- name: ListRecentSales :many
-- Recent sales feed on the sales dashboard.
-- Member name is stored encrypted; the service layer decrypts via crypto.Enc.
SELECT
  t.id            AS tx_id,
  t.amount        AS tx_amount,
  t.created_at    AS tx_created_at,
  t.member_id     AS member_id,
  mb.name         AS member_name,
  m.plan_snapshot AS plan_snapshot
FROM transactions t
LEFT JOIN memberships m ON m.charge_tx_id = t.id
LEFT JOIN members mb    ON mb.id           = t.member_id
WHERE t.type = 'charge'
  AND t.created_at >= ?
ORDER BY t.created_at DESC, t.id DESC
LIMIT 20;

-- name: ListExpiringMemberships :many
-- Memberships whose purchased_at + 1 year expiration falls into [?, ?).
-- Active status only. Sorted by soonest expiration first.
SELECT
  m.id            AS membership_id,
  m.member_id     AS member_id,
  m.plan_snapshot AS plan_snapshot,
  m.purchased_at  AS purchased_at,
  mb.name         AS member_name
FROM memberships m
JOIN members mb ON mb.id = m.member_id
WHERE m.status = 'active'
  AND datetime(m.purchased_at, '+1 year') >= datetime(sqlc.arg(purchased_at))
  AND datetime(m.purchased_at, '+1 year') < datetime(sqlc.arg(purchased_at_2))
ORDER BY datetime(m.purchased_at, '+1 year') ASC, m.id ASC;

-- name: ListDormantMembers :many
-- Active members (is_active=1) with balance > 0 whose last transaction is older
-- than the cutoff, or who have no transactions at all. Limit 20.
SELECT
  m.id,
  m.name,
  m.balance,
  MAX(t.created_at) AS last_tx_at
FROM members m
LEFT JOIN transactions t ON t.member_id = m.id
WHERE m.is_active = 1 AND m.balance > 0
GROUP BY m.id
HAVING (MAX(t.created_at) IS NULL OR MAX(t.created_at) < ?)
ORDER BY
  CASE WHEN MAX(t.created_at) IS NULL THEN 0 ELSE 1 END,
  MAX(t.created_at) ASC
LIMIT 20;
