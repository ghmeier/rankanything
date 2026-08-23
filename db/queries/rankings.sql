-- name: CreateRanking :one
INSERT INTO rankings (name, description, user_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetRankingByUUID :one
SELECT * FROM rankings WHERE uuid = $1;

-- The public share route resolves a ranking_shares row by public_slug,
-- which only carries the ranking's internal id, not its external uuid.
-- name: GetRankingByID :one
SELECT * FROM rankings WHERE id = $1;

-- name: ListRankingsByUser :many
SELECT * FROM rankings WHERE user_id = $1 ORDER BY updated_at DESC;

-- name: UpdateRanking :one
UPDATE rankings
SET name = $2, description = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteRanking :exec
DELETE FROM rankings WHERE id = $1;
