-- name: AddRankingItemToTier :one
INSERT INTO ranking_item_tiers (ranking_item_id, ranking_tier_id, ranking_version_id, position)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: RemoveRankingItemFromTier :exec
DELETE FROM ranking_item_tiers WHERE ranking_item_id = $1 AND ranking_tier_id = $2;

-- name: RemoveRankingItemFromAllTiers :exec
DELETE FROM ranking_item_tiers WHERE ranking_item_id = $1;

-- These reorders rely on a deferred unique index so that rankings can share
-- positions within a transaction, but must end up unique.
-- name: ReorderRankingItemTier :one
UPDATE ranking_item_tiers
SET position = $2
WHERE id = $1
RETURNING *;

-- name: ListRankingItemTiersForTier :many
SELECT * FROM ranking_item_tiers WHERE ranking_tier_id = $1 ORDER BY position;

-- Gets every ranked item tier placement for a verion, ordered by position within
-- tier and with respect to tier position.
-- name: ListRankingItemTiersForVersion :many
SELECT it.*
FROM ranking_item_tiers it
JOIN ranking_tiers t ON t.id = it.ranking_tier_id
WHERE it.ranking_version_id = $1
ORDER BY t.position, it.position;

-- name: NextRankingItemTierPosition :one
SELECT coalesce(max(position) + 1, 0)::smallint FROM ranking_item_tiers WHERE ranking_tier_id = $1;
