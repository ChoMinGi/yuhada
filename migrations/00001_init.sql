-- +goose Up
-- 기존 Postgres 스키마를 SQLite 방언으로 이식.
-- 주요 차이:
--   - uuid(pg) → text (app에서 google/uuid로 생성)
--   - timestamptz(pg) → text (ISO8601, CURRENT_TIMESTAMP이 '2026-04-19 15:45:00' 형태)
--   - gen_random_uuid() / jsonb / PL-pgSQL / RLS 제거 → 앱 레이어에서 처리
--   - guard_balance_update 트리거 제거 → service.Charge/Deduct에서 `BEGIN IMMEDIATE` 트랜잭션으로 보장

-- +goose StatementBegin
CREATE TABLE members (
  id         TEXT PRIMARY KEY,              -- UUID (app 생성)
  name       TEXT NOT NULL,
  phone      TEXT NOT NULL UNIQUE,          -- 숫자만 저장 ('01012345678')
  card_uuid  TEXT UNIQUE,                   -- NFC UUID. null = 카드 미발급
  balance    INTEGER NOT NULL DEFAULT 0 CHECK (balance >= 0),
  memo       TEXT,
  is_active  INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd

CREATE INDEX idx_members_phone     ON members(phone);
CREATE INDEX idx_members_card_uuid ON members(card_uuid) WHERE card_uuid IS NOT NULL;
CREATE INDEX idx_members_name      ON members(name);
CREATE INDEX idx_members_created   ON members(created_at DESC);

-- updated_at 자동 갱신 트리거
-- +goose StatementBegin
CREATE TRIGGER trg_members_updated_at
AFTER UPDATE ON members
FOR EACH ROW
WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE members
     SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
   WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- 거래 내역 (append-only). balance_after 감사용.
-- +goose StatementBegin
CREATE TABLE transactions (
  id            TEXT PRIMARY KEY,
  member_id     TEXT NOT NULL REFERENCES members(id) ON DELETE RESTRICT,
  type          TEXT NOT NULL CHECK (type IN ('charge', 'deduct', 'refund')),
  amount        INTEGER NOT NULL CHECK (amount > 0),
  balance_after INTEGER NOT NULL CHECK (balance_after >= 0),
  memo          TEXT,
  created_by    TEXT,                       -- admin_users.id (null = system/bootstrap)
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd

CREATE INDEX idx_tx_member_created ON transactions(member_id, created_at DESC);
CREATE INDEX idx_tx_created        ON transactions(created_at DESC);
CREATE INDEX idx_tx_type_created   ON transactions(type, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_tx_type_created;
DROP INDEX IF EXISTS idx_tx_created;
DROP INDEX IF EXISTS idx_tx_member_created;
DROP TABLE IF EXISTS transactions;

DROP TRIGGER IF EXISTS trg_members_updated_at;
DROP INDEX IF EXISTS idx_members_created;
DROP INDEX IF EXISTS idx_members_name;
DROP INDEX IF EXISTS idx_members_card_uuid;
DROP INDEX IF EXISTS idx_members_phone;
DROP TABLE IF EXISTS members;
