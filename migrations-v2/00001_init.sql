-- +goose Up
-- v2 통합 초기화 스키마.
-- v1의 00001~00005 마이그레이션을 하나로 병합한 뒤 membership_plans / memberships 두 테이블을 추가한다.
-- idempotent 하도록 모든 CREATE는 IF NOT EXISTS 를 사용 (R7: main.go 자동 마이그 안전성).
-- v1 원본 마이그와 컬럼 정의는 1:1 일치 — v2 격리를 위해 별도 DB 파일에 동일 스키마로 복제.

-- ═════════════════════════════════════════════════════════════
-- 1. members (v1 00001_init.sql)
-- ═════════════════════════════════════════════════════════════
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS members (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  phone         TEXT NOT NULL UNIQUE,
  card_uuid     TEXT UNIQUE,
  balance       INTEGER NOT NULL DEFAULT 0 CHECK (balance >= 0),
  memo          TEXT,
  is_active     INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  kakao_user_id TEXT
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_members_phone     ON members(phone);
CREATE INDEX IF NOT EXISTS idx_members_card_uuid ON members(card_uuid) WHERE card_uuid IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_members_name      ON members(name);
CREATE INDEX IF NOT EXISTS idx_members_created   ON members(created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_members_kakao_user_id ON members(kakao_user_id) WHERE kakao_user_id IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_members_updated_at
AFTER UPDATE ON members
FOR EACH ROW
WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE members
     SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
   WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- ═════════════════════════════════════════════════════════════
-- 2. transactions (v1 00001_init.sql)
-- ═════════════════════════════════════════════════════════════
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS transactions (
  id            TEXT PRIMARY KEY,
  member_id     TEXT NOT NULL REFERENCES members(id) ON DELETE RESTRICT,
  type          TEXT NOT NULL CHECK (type IN ('charge', 'deduct', 'refund')),
  amount        INTEGER NOT NULL CHECK (amount > 0),
  balance_after INTEGER NOT NULL CHECK (balance_after >= 0),
  memo          TEXT,
  created_by    TEXT,
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_tx_member_created ON transactions(member_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tx_created        ON transactions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tx_type_created   ON transactions(type, created_at DESC);

-- ═════════════════════════════════════════════════════════════
-- 3. admin_users (v1 00002 + 00005 pin_hash 병합)
-- ═════════════════════════════════════════════════════════════
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS admin_users (
  id            TEXT PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  pw_hash       TEXT NOT NULL,
  name          TEXT,
  role          TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'staff')),
  is_active     INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  last_login_at TEXT,
  pin_hash      TEXT
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_admin_email ON admin_users(email);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_admin_users_updated_at
AFTER UPDATE ON admin_users
FOR EACH ROW
WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE admin_users
     SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
   WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- ═════════════════════════════════════════════════════════════
-- 4. kakao_verify_requests (v1 00003)
-- ═════════════════════════════════════════════════════════════
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS kakao_verify_requests (
  id                  TEXT PRIMARY KEY,
  kakao_user_id       TEXT NOT NULL,
  kakao_name          TEXT,
  kakao_email         TEXT,
  claimed_name        TEXT,
  claimed_phone_last4 TEXT,
  status              TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'approved', 'rejected')),
  member_id           TEXT REFERENCES members(id),
  reviewed_by         TEXT REFERENCES admin_users(id),
  reviewed_at         TEXT,
  reason              TEXT,
  created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_verify_status     ON kakao_verify_requests(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_verify_kakao_user ON kakao_verify_requests(kakao_user_id);

-- ═════════════════════════════════════════════════════════════
-- 5. sessions (v1 00004 — scs/sqlite3store 표준)
-- ═════════════════════════════════════════════════════════════
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS sessions (
  token  TEXT PRIMARY KEY,
  data   BLOB NOT NULL,
  expiry REAL NOT NULL
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expiry);

-- ═════════════════════════════════════════════════════════════
-- 6. membership_plans (v2 신규 — 가격표 SKU)
--    pricing-model.md §2와 1:1 일치
-- ═════════════════════════════════════════════════════════════
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS membership_plans (
  id              TEXT PRIMARY KEY,
  tier_label      TEXT NOT NULL,                       -- '20만원 회원권'
  tier_amount     INTEGER NOT NULL,                    -- 200000 (그룹/정렬용)
  payment_method  TEXT NOT NULL CHECK (payment_method IN ('cash','card')),
  price           INTEGER NOT NULL,                    -- 손님이 실제 내는 돈 = 세금 신고 매출
  credit_amount   INTEGER NOT NULL,                    -- 잔액으로 적립되는 금액 = 선수금(부채)
  perk_label      TEXT,                                -- 증정품 자유 텍스트 (NULL 가능)
  display_order   INTEGER NOT NULL DEFAULT 0,
  is_active       INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0,1)),
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_plans_active_order ON membership_plans(is_active, display_order);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_plans_updated_at
AFTER UPDATE ON membership_plans
FOR EACH ROW
WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE membership_plans
     SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
   WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- ═════════════════════════════════════════════════════════════
-- 7. memberships (v2 신규 — 판매 이력, 충전 시 자동 INSERT)
--    R5: status CHECK 제약 명시 (active/refunded/expired/transferred)
-- ═════════════════════════════════════════════════════════════
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS memberships (
  id            TEXT PRIMARY KEY,
  member_id     TEXT NOT NULL REFERENCES members(id) ON DELETE RESTRICT,
  plan_id       TEXT REFERENCES membership_plans(id), -- NULL 허용: 플랜 삭제 후에도 이력 보존
  plan_snapshot TEXT NOT NULL,                         -- JSON, 발급 시점 가격/혜택 박제
  charge_tx_id  TEXT NOT NULL REFERENCES transactions(id),
  status        TEXT NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','refunded','expired','transferred')),
  purchased_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  created_by    TEXT
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_memberships_member_purchased ON memberships(member_id, purchased_at DESC);
CREATE INDEX IF NOT EXISTS idx_memberships_status           ON memberships(status);
CREATE INDEX IF NOT EXISTS idx_memberships_charge_tx        ON memberships(charge_tx_id);

-- +goose Down
DROP INDEX IF EXISTS idx_memberships_charge_tx;
DROP INDEX IF EXISTS idx_memberships_status;
DROP INDEX IF EXISTS idx_memberships_member_purchased;
DROP TABLE IF EXISTS memberships;

DROP TRIGGER IF EXISTS trg_plans_updated_at;
DROP INDEX IF EXISTS idx_plans_active_order;
DROP TABLE IF EXISTS membership_plans;

DROP INDEX IF EXISTS idx_sessions_expiry;
DROP TABLE IF EXISTS sessions;

DROP INDEX IF EXISTS idx_verify_kakao_user;
DROP INDEX IF EXISTS idx_verify_status;
DROP TABLE IF EXISTS kakao_verify_requests;

DROP TRIGGER IF EXISTS trg_admin_users_updated_at;
DROP INDEX IF EXISTS idx_admin_email;
DROP TABLE IF EXISTS admin_users;

DROP INDEX IF EXISTS idx_tx_type_created;
DROP INDEX IF EXISTS idx_tx_created;
DROP INDEX IF EXISTS idx_tx_member_created;
DROP TABLE IF EXISTS transactions;

DROP TRIGGER IF EXISTS trg_members_updated_at;
DROP INDEX IF EXISTS uq_members_kakao_user_id;
DROP INDEX IF EXISTS idx_members_created;
DROP INDEX IF EXISTS idx_members_name;
DROP INDEX IF EXISTS idx_members_card_uuid;
DROP INDEX IF EXISTS idx_members_phone;
DROP TABLE IF EXISTS members;
