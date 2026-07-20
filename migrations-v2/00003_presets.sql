-- +goose Up
-- v2 preset tables for the settings sub-menu:
--   1) v2_deduct_amount_presets  - shortcut buttons on the deduct modal
--   2) v2_service_memo_presets   - tag buttons on the deduct modal
-- idempotent CREATE TABLE / CREATE INDEX so main.go auto-migration is safe (R7).
-- ASCII-only SQL comments to avoid the sqlc parser bug reported in Phase 2.

-- ==============================================================
-- 1. v2_deduct_amount_presets
-- ==============================================================
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS v2_deduct_amount_presets (
  id            TEXT PRIMARY KEY,
  amount        INTEGER NOT NULL CHECK (amount > 0),
  display_order INTEGER NOT NULL DEFAULT 0,
  is_active     INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_v2_dap_active_order ON v2_deduct_amount_presets(is_active, display_order);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_v2_dap_updated_at
AFTER UPDATE ON v2_deduct_amount_presets
FOR EACH ROW
WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE v2_deduct_amount_presets
     SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
   WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- ==============================================================
-- 2. v2_service_memo_presets
-- ==============================================================
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS v2_service_memo_presets (
  id            TEXT PRIMARY KEY,
  label         TEXT NOT NULL,
  display_order INTEGER NOT NULL DEFAULT 0,
  is_active     INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_v2_smp_active_order ON v2_service_memo_presets(is_active, display_order);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_v2_smp_updated_at
AFTER UPDATE ON v2_service_memo_presets
FOR EACH ROW
WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE v2_service_memo_presets
     SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
   WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- ==============================================================
-- 3. Seed data
-- INSERT OR IGNORE keeps this migration idempotent under auto-migrate.
-- ==============================================================
-- +goose StatementBegin
INSERT OR IGNORE INTO v2_deduct_amount_presets (id, amount, display_order) VALUES
  ('dap-20k', 20000, 10),
  ('dap-23k', 23000, 20),
  ('dap-25k', 25000, 30),
  ('dap-44k', 44000, 40),
  ('dap-55k', 55000, 50);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT OR IGNORE INTO v2_service_memo_presets (id, label, display_order) VALUES
  ('smp-cut',    '컷',       10),
  ('smp-perm',   '펌',       20),
  ('smp-magic',  '매직',     30),
  ('smp-clinic', '클리닉',   40),
  ('smp-color',  '염색',     50),
  ('smp-root',   '뿌리염색', 60);
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_v2_smp_updated_at;
DROP INDEX IF EXISTS idx_v2_smp_active_order;
DROP TABLE IF EXISTS v2_service_memo_presets;

DROP TRIGGER IF EXISTS trg_v2_dap_updated_at;
DROP INDEX IF EXISTS idx_v2_dap_active_order;
DROP TABLE IF EXISTS v2_deduct_amount_presets;
