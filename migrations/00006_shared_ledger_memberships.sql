-- +goose Up
-- Add operational membership metadata beside the existing canonical ledger.
-- Existing members/transactions and every test-only v2_* table are untouched.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS membership_plans (
  id              TEXT PRIMARY KEY,
  tier_label      TEXT NOT NULL,
  tier_amount     INTEGER NOT NULL CHECK (tier_amount > 0),
  payment_method  TEXT NOT NULL CHECK (payment_method IN ('cash', 'card')),
  price           INTEGER NOT NULL CHECK (price > 0),
  credit_amount   INTEGER NOT NULL CHECK (credit_amount > 0),
  perk_label      TEXT,
  display_order   INTEGER NOT NULL DEFAULT 0,
  is_active       INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd
CREATE INDEX IF NOT EXISTS idx_membership_plans_active_order ON membership_plans(is_active, display_order);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_membership_plans_updated_at
AFTER UPDATE ON membership_plans FOR EACH ROW WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE membership_plans SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS memberships (
  id            TEXT PRIMARY KEY,
  member_id     TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  plan_id       TEXT REFERENCES membership_plans(id) ON DELETE SET NULL,
  plan_snapshot TEXT NOT NULL CHECK (json_valid(plan_snapshot)),
  charge_tx_id  TEXT NOT NULL UNIQUE REFERENCES transactions(id) ON DELETE CASCADE,
  status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'refunded', 'expired', 'transferred')),
  purchased_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  created_by    TEXT
);
-- +goose StatementEnd
CREATE INDEX IF NOT EXISTS idx_memberships_member_purchased ON memberships(member_id, purchased_at DESC);
CREATE INDEX IF NOT EXISTS idx_memberships_status ON memberships(status);

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS deduct_amount_presets (
  id            TEXT PRIMARY KEY,
  amount        INTEGER NOT NULL CHECK (amount > 0),
  display_order INTEGER NOT NULL DEFAULT 0,
  is_active     INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd
CREATE INDEX IF NOT EXISTS idx_deduct_amount_presets_active_order ON deduct_amount_presets(is_active, display_order);
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_deduct_amount_presets_updated_at
AFTER UPDATE ON deduct_amount_presets FOR EACH ROW WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE deduct_amount_presets SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS service_memo_presets (
  id            TEXT PRIMARY KEY,
  label         TEXT NOT NULL,
  display_order INTEGER NOT NULL DEFAULT 0,
  is_active     INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd
CREATE INDEX IF NOT EXISTS idx_service_memo_presets_active_order ON service_memo_presets(is_active, display_order);
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_service_memo_presets_updated_at
AFTER UPDATE ON service_memo_presets FOR EACH ROW WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE service_memo_presets SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- Dedicated persistent UI sessions; neither v1 sessions nor demo v2_sessions is reused.
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS admin_sessions_v2 (
  token  TEXT PRIMARY KEY,
  data   BLOB NOT NULL,
  expiry REAL NOT NULL
);
-- +goose StatementEnd
CREATE INDEX IF NOT EXISTS idx_admin_sessions_v2_expiry ON admin_sessions_v2(expiry);

-- Seed only newly introduced operational tables. Never infer old sales from credits.
-- +goose StatementBegin
INSERT OR IGNORE INTO membership_plans
  (id, tier_label, tier_amount, payment_method, price, credit_amount, perk_label, display_order)
VALUES
  ('plan-20-card', '20만원 회원권', 200000, 'card', 200000, 220000, NULL, 11),
  ('plan-20-cash', '20만원 회원권', 200000, 'cash', 200000, 240000, NULL, 12),
  ('plan-30-card', '30만원 회원권', 300000, 'card', 300000, 330000, '홈케어 18,000원 증정', 21),
  ('plan-30-cash', '30만원 회원권', 300000, 'cash', 300000, 360000, '홈케어 18,000원 증정', 22),
  ('plan-50-card', '50만원 회원권', 500000, 'card', 500000, 550000, '모로칸 오일 32,000원 증정', 31),
  ('plan-50-cash', '50만원 회원권', 500000, 'cash', 500000, 600000, '모로칸 오일 32,000원 증정', 32);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT OR IGNORE INTO deduct_amount_presets (id, amount, display_order) VALUES
  ('dap-20k', 20000, 10), ('dap-23k', 23000, 20), ('dap-25k', 25000, 30),
  ('dap-44k', 44000, 40), ('dap-55k', 55000, 50);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT OR IGNORE INTO service_memo_presets (id, label, display_order) VALUES
  ('smp-cut', '컷', 10), ('smp-perm', '펌', 20), ('smp-magic', '매직', 30),
  ('smp-clinic', '클리닉', 40), ('smp-color', '염색', 50), ('smp-root', '뿌리염색', 60);
-- +goose StatementEnd

-- +goose Down
-- Roll back the UI with its feature flag/previous binary, not by deleting sales.
-- Preserve newly recorded operational metadata and sessions on schema rollback.
SELECT 1;
