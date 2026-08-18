-- name: CreateRanking :one
INSERT INTO rankings (title, user_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetRankingBySlug :one
SELECT * FROM rankings WHERE slug = $1;

-- name: EnsureDraftRankingForSlug :one
SELECT EXISTS (
    SELECT 1  FROM rankings WHERE slug = $1 AND user_id IS NULL
);

-- name: EnsureUserRankingForSlug :one
SELECT EXISTS (
    SELECT 1  FROM rankings WHERE slug = $1 AND user_id = $2
);

-- name: UpdateRanking :one
UPDATE rankings
SET title = $2, description = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClaimRankings :many
UPDATE rankings
SET user_id = $1, updated_at = now()
WHERE slug = ANY(@slugs::uuid[]) AND user_id IS NULL
RETURNING *;

-- name: ListRankingsByUser :many
SELECT * FROM rankings WHERE user_id = $1 ORDER BY updated_at DESC;

-- name: DeleteStaleDrafts :exec
DELETE FROM rankings
WHERE user_id IS NULL AND created_at < now() - interval '30 days';

-- name: CreateTier :one
INSERT INTO ranking_tiers (ranking_id, label, position, color, allow_multiple)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListTiers :many
SELECT * FROM ranking_tiers WHERE ranking_id = $1 ORDER BY position;

-- name: GetTier :one
SELECT * FROM ranking_tiers WHERE id = $1;

-- name: UpdateTier :one
UPDATE ranking_tiers
SET label = $2, color = $3, position = $4, allow_multiple = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteTier :exec
DELETE FROM ranking_tiers WHERE id = $1;

-- name: NextTierPosition :one
SELECT coalesce(max(position) + 1, 0)::int FROM ranking_tiers WHERE ranking_id = $1;
