-- +goose Up
-- 고객 카카오 계정 연동 (카드 분실 재발급 본인 인증).
-- 한 번이라도 매칭 성공 시 kakao_user_id를 저장해서 이후엔 이것만 사용.

ALTER TABLE members ADD COLUMN kakao_user_id TEXT;
CREATE UNIQUE INDEX uq_members_kakao_user_id ON members(kakao_user_id) WHERE kakao_user_id IS NOT NULL;

-- 본인 인증 매칭 감사 로그 (사장님 수동 승인 플로우 포함)
-- +goose StatementBegin
CREATE TABLE kakao_verify_requests (
  id           TEXT PRIMARY KEY,
  kakao_user_id TEXT NOT NULL,
  kakao_name    TEXT,
  kakao_email   TEXT,
  claimed_name  TEXT,                        -- 고객이 주장한 이름 (fallback 3단계)
  claimed_phone_last4 TEXT,                  -- 전화 뒷 4자리
  status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'approved', 'rejected')),
  member_id     TEXT REFERENCES members(id),  -- 승인 시 매칭된 회원
  reviewed_by   TEXT REFERENCES admin_users(id),
  reviewed_at   TEXT,
  reason        TEXT,                         -- 거부 사유
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementEnd

CREATE INDEX idx_verify_status      ON kakao_verify_requests(status, created_at DESC);
CREATE INDEX idx_verify_kakao_user  ON kakao_verify_requests(kakao_user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_verify_kakao_user;
DROP INDEX IF EXISTS idx_verify_status;
DROP TABLE IF EXISTS kakao_verify_requests;
DROP INDEX IF EXISTS uq_members_kakao_user_id;
-- SQLite는 DROP COLUMN 지원 (3.35+). macOS/Cafe24 기본 버전 OK.
ALTER TABLE members DROP COLUMN kakao_user_id;
