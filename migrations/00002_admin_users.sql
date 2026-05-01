-- +goose Up
-- 사장님/직원 계정. 1차는 사장님 1명만, 추후 직원 추가 가능한 구조.

-- +goose StatementBegin
CREATE TABLE admin_users (
  id         TEXT PRIMARY KEY,                -- UUID
  email      TEXT NOT NULL UNIQUE,
  pw_hash    TEXT NOT NULL,                   -- bcrypt cost 12+
  name       TEXT,                            -- 표시용 이름 (optional)
  role       TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'staff')),
  is_active  INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  last_login_at TEXT
);
-- +goose StatementEnd

CREATE INDEX idx_admin_email ON admin_users(email);

-- +goose StatementBegin
CREATE TRIGGER trg_admin_users_updated_at
AFTER UPDATE ON admin_users
FOR EACH ROW
WHEN OLD.updated_at = NEW.updated_at
BEGIN
  UPDATE admin_users
     SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
   WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_admin_users_updated_at;
DROP INDEX IF EXISTS idx_admin_email;
DROP TABLE IF EXISTS admin_users;
