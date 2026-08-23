-- name: CreateRankingTier :one
INSERT INTO ranking_tiers (ranking_version_id, ranking_id, title, color_hex, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListRankingTiersForVersion :many
SELECT * FROM ranking_tiers WHERE ranking_version_id = $1 ORDER BY position;

-- name: GetRankingTier :one
SELECT * FROM ranking_tiers WHERE id = $1;

-- name: UpdateRankingTier :one
UPDATE ranking_tiers
SET title = $2, color_hex = $3, position = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteRankingTier :exec
DELETE FROM ranking_tiers WHERE id = $1;

-- name: NextRankingTierPosition :one
SELECT coalesce(max(position) + 1, 0)::smallint FROM ranking_tiers WHERE ranking_version_id = $1;
