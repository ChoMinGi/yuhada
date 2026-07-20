-- v2 preset CRUD queries for the settings sub-menu.
-- Two tables share the same shape (list/create/update/deactivate/reactivate).
-- ASCII-only SQL comments to sidestep the sqlc parser bug reported in Phase 2.

-- ==============================================================
-- v2_deduct_amount_presets
-- ==============================================================

-- name: ListActiveDeductPresets :many
-- Consumed by the deduct modal to render shortcut amount buttons.
SELECT * FROM v2_deduct_amount_presets
WHERE is_active = 1
ORDER BY display_order ASC;

-- name: ListAllDeductPresets :many
-- Owner-facing settings screen (includes inactive rows).
SELECT * FROM v2_deduct_amount_presets
ORDER BY display_order ASC;

-- name: CreateDeductPreset :one
INSERT INTO v2_deduct_amount_presets
  (id, amount, display_order, is_active)
VALUES (?, ?, ?, 1)
RETURNING *;

-- name: UpdateDeductPreset :exec
UPDATE v2_deduct_amount_presets
   SET amount        = ?,
       display_order = ?
 WHERE id = ?;

-- name: DeactivateDeductPreset :exec
UPDATE v2_deduct_amount_presets SET is_active = 0 WHERE id = ?;

-- name: ReactivateDeductPreset :exec
UPDATE v2_deduct_amount_presets SET is_active = 1 WHERE id = ?;

-- name: UpdateDeductPresetOrder :exec
-- Used by drag-and-drop reorder (does not touch amount).
UPDATE v2_deduct_amount_presets SET display_order = ? WHERE id = ?;

-- name: DeleteDeductPreset :exec
-- Hard delete. No foreign key references from other tables (only used as UI preset).
DELETE FROM v2_deduct_amount_presets WHERE id = ?;

-- ==============================================================
-- v2_service_memo_presets
-- ==============================================================

-- name: ListActiveServiceMemoPresets :many
-- Consumed by the deduct modal to render service tag buttons.
SELECT * FROM v2_service_memo_presets
WHERE is_active = 1
ORDER BY display_order ASC;

-- name: ListAllServiceMemoPresets :many
-- Owner-facing settings screen (includes inactive rows).
SELECT * FROM v2_service_memo_presets
ORDER BY display_order ASC;

-- name: CreateServiceMemoPreset :one
INSERT INTO v2_service_memo_presets
  (id, label, display_order, is_active)
VALUES (?, ?, ?, 1)
RETURNING *;

-- name: UpdateServiceMemoPreset :exec
UPDATE v2_service_memo_presets
   SET label         = ?,
       display_order = ?
 WHERE id = ?;

-- name: DeactivateServiceMemoPreset :exec
UPDATE v2_service_memo_presets SET is_active = 0 WHERE id = ?;

-- name: ReactivateServiceMemoPreset :exec
UPDATE v2_service_memo_presets SET is_active = 1 WHERE id = ?;

-- name: UpdateServiceMemoPresetOrder :exec
-- Used by drag-and-drop reorder (does not touch label).
UPDATE v2_service_memo_presets SET display_order = ? WHERE id = ?;

-- name: DeleteServiceMemoPreset :exec
-- Hard delete. No foreign key references (only used as UI preset).
DELETE FROM v2_service_memo_presets WHERE id = ?;
