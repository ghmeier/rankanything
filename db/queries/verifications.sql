-- name: CreateEmailVerification :one
INSERT INTO email_verifications (token_hash, expires_at, user_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetEmailVerificationByTokenHash :one
SELECT * FROM email_verifications WHERE token_hash = $1;

-- Marks a verification used. Returns nothing if no verification was updated
-- beacuse it's expired, invalid or already used.
-- name: RedeemEmailVerification :one
UPDATE email_verifications
SET is_verified = true
WHERE token_hash = $1
  AND is_verified = false
  AND expires_at > now()
RETURNING *;

-- name: CreatePasswordReset :one
INSERT INTO password_resets (token_hash, expires_at, user_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetPasswordResetByTokenHash :one
SELECT * FROM password_resets WHERE token_hash = $1;

-- Same as above but for password verifications.
-- name: RedeemPasswordReset :one
UPDATE password_resets
SET is_used = true
WHERE token_hash = $1
  AND is_used = false
  AND expires_at > now()
RETURNING *;
