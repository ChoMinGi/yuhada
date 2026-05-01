-- name: CreateVerifyRequest :one
INSERT INTO kakao_verify_requests
  (id, kakao_user_id, kakao_name, kakao_email, claimed_name, claimed_phone_last4)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListPendingVerifyRequests :many
SELECT * FROM kakao_verify_requests
WHERE status = 'pending'
ORDER BY created_at DESC;

-- name: GetVerifyRequest :one
SELECT * FROM kakao_verify_requests WHERE id = ? LIMIT 1;

-- name: ApproveVerifyRequest :exec
UPDATE kakao_verify_requests
   SET status = 'approved',
       member_id = ?,
       reviewed_by = ?,
       reviewed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: RejectVerifyRequest :exec
UPDATE kakao_verify_requests
   SET status = 'rejected',
       reason = ?,
       reviewed_by = ?,
       reviewed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;
