-- v2 v2_memberships queries.
-- plan_snapshot is a JSON blob written by the service layer at charge time
-- so historical rows survive plan edits/deletion (see pricing-model.md section 4).

-- name: CreateMembership :one
INSERT INTO v2_memberships
  (id, member_id, plan_id, plan_snapshot, charge_tx_id, status, created_by)
VALUES (?, ?, ?, ?, ?, 'active', ?)
RETURNING *;

-- name: GetMembership :one
SELECT * FROM v2_memberships WHERE id = ? LIMIT 1;

-- name: ListMembershipsByMember :many
-- Powers the MEMBERSHIPS section on the member detail screen.
-- Ordered latest-first.
SELECT * FROM v2_memberships
WHERE member_id = ?
ORDER BY purchased_at DESC;

-- name: UpdateMembershipStatus :exec
-- Refund/expire/transfer transitions (R5).
-- The service layer MUST wrap this together with the v2_transactions
-- and v2_members.balance updates in a single BEGIN IMMEDIATE tx.
UPDATE v2_memberships SET status = ? WHERE id = ?;

-- name: ListChargeHistoryWithPlan :many
-- R3: v2_transactions LEFT JOIN v2_memberships convention (pricing-model.md section 8).
-- Used by the recent-v2_transactions section on member detail so the UI can
-- render a plan tier badge when plan_snapshot IS NOT NULL, and fall back
-- to a plain "generic charge" line when it is NULL.
SELECT
  t.id            AS tx_id,
  t.member_id     AS tx_member_id,
  t.type          AS tx_type,
  t.amount        AS tx_amount,
  t.balance_after AS tx_balance_after,
  t.memo          AS tx_memo,
  t.created_by    AS tx_created_by,
  t.created_at    AS tx_created_at,
  m.id            AS membership_id,
  m.plan_id       AS plan_id,
  m.plan_snapshot AS plan_snapshot,
  m.status        AS membership_status,
  m.purchased_at  AS purchased_at
FROM v2_transactions t
LEFT JOIN v2_memberships m ON m.charge_tx_id = t.id
WHERE t.member_id = ?
  AND t.type      = 'charge'
ORDER BY t.created_at DESC
LIMIT 20;

-- name: SumMembershipRevenueSince :one
-- Revenue stats for tax reporting: revenue = SUM(price),
-- liability = SUM(credit_amount), across active v2_memberships since ?.
SELECT
  COALESCE(SUM(CAST(json_extract(plan_snapshot, '$.price') AS INTEGER)), 0) AS revenue,
  COALESCE(SUM(CAST(json_extract(plan_snapshot, '$.credit_amount') AS INTEGER)), 0) AS liability,
  COUNT(*) AS sold
FROM v2_memberships
WHERE purchased_at >= ?
  AND status = 'active';
