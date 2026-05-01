-- name: CreateAdminUser :one
INSERT INTO admin_users (id, email, pw_hash, name, role)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAdminByEmail :one
SELECT * FROM admin_users
WHERE email = ? AND is_active = 1
LIMIT 1;

-- name: ListActiveAdmins :many
SELECT * FROM admin_users WHERE is_active = 1 ORDER BY created_at ASC;

-- name: SetAdminPin :exec
UPDATE admin_users SET pin_hash = ? WHERE id = ?;

-- name: GetAdmin :one
SELECT * FROM admin_users WHERE id = ? LIMIT 1;

-- name: UpdateAdminLastLogin :exec
UPDATE admin_users SET last_login_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?;

-- name: CountAdminUsers :one
SELECT COUNT(*) FROM admin_users;
