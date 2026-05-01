-- +goose Up
-- scs/sqlite3store 표준 스키마.
-- https://pkg.go.dev/github.com/alexedwards/scs/sqlite3store

-- +goose StatementBegin
CREATE TABLE sessions (
  token  TEXT PRIMARY KEY,
  data   BLOB NOT NULL,
  expiry REAL NOT NULL
);
-- +goose StatementEnd

CREATE INDEX idx_sessions_expiry ON sessions(expiry);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_expiry;
DROP TABLE IF EXISTS sessions;
