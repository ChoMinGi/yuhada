-- v2 membership_plans CRUD queries.
-- See references/pricing-model.md for column semantics.

-- name: ListActivePlans :many
-- Used by charge modal (6 tier cards) and dashboard selects.
-- Only is_active=1, sorted by display_order asc (20-card, 20-cash, 30-card, ...).
SELECT * FROM membership_plans
WHERE is_active = 1
ORDER BY display_order ASC;

-- name: ListAllPlans :many
-- Owner-facing price-list management screen; includes inactive plans.
SELECT * FROM membership_plans
ORDER BY display_order ASC;

-- name: GetPlan :one
SELECT * FROM membership_plans WHERE id = ? LIMIT 1;

-- name: CreatePlan :one
INSERT INTO membership_plans
  (id, tier_label, tier_amount, payment_method, price, credit_amount, perk_label, display_order, is_active)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
RETURNING *;

-- name: UpdatePlan :exec
UPDATE membership_plans
   SET tier_label     = ?,
       tier_amount    = ?,
       payment_method = ?,
       price          = ?,
       credit_amount  = ?,
       perk_label     = ?,
       display_order  = ?
 WHERE id = ?;

-- name: DeactivatePlan :exec
UPDATE membership_plans SET is_active = 0 WHERE id = ?;

-- name: ReactivatePlan :exec
UPDATE membership_plans SET is_active = 1 WHERE id = ?;
