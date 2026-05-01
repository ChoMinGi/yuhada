-- +goose Up
-- 6자리 PIN 로그인 도입.
-- pw_hash는 유지 (마이그레이션 안전 + 추후 비번 fallback 옵션).

ALTER TABLE admin_users ADD COLUMN pin_hash TEXT;

-- pin 검증은 ListActiveAdmins 후 in-memory 비교.
-- 인덱스는 불필요 (사용자 1~5명).

-- +goose Down
ALTER TABLE admin_users DROP COLUMN pin_hash;
