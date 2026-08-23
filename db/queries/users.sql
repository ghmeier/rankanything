-- name: CreateUser :one
INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: TouchLastLogin :exec
UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1;

-- name: MarkUserEmailVerified :one
UPDATE users SET email_verified = true, updated_at = now() WHERE id = $1
RETURNING *;

-- name: UpdateUserPasswordHash :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: UpdateUserThemePreference :one
UPDATE users SET theme_preference = $2, updated_at = now() WHERE id = $1
RETURNING *;
