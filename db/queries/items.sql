-- name: CreateItem :one
INSERT INTO ranked_items (label, sublabel, source_url, source_image_url, image_url)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: AddItemToRanking :exec
INSERT INTO ranking_items (ranking_id, ranked_item_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveItemFromRanking :exec
DELETE FROM ranking_items WHERE ranking_id = $1 AND ranked_item_id = $2;

-- name: ListRankingItems :many
SELECT i.* FROM ranked_items i
JOIN ranking_items ri ON ri.ranked_item_id = i.id
WHERE ri.ranking_id = $1
ORDER BY ri.created_at, i.id;

-- name: ListRankingTierItems :many
SELECT i.* FROM ranked_items i
JOIN ranking_tier_items rti ON rti.ranked_item_id = i.id
WHERE rti.ranking_tier_id = $1
ORDER BY rti.created_at, i.id;

-- name: ListPlacements :many
SELECT ti.ranking_tier_id, ti.ranked_item_id, ti.position
FROM ranking_tier_items ti
JOIN ranking_tiers t ON t.id = ti.ranking_tier_id
WHERE t.ranking_id = $1
ORDER BY t.position, ti.position;

-- name: CountTierItems :one
SELECT count(*)::int FROM ranking_tier_items WHERE ranking_tier_id = $1;



-- name: ClearTierPlacements :exec
DELETE FROM ranking_tier_items WHERE ranking_tier_id = $1;

-- name: RemoveItemFromTiers :exec
DELETE FROM ranking_tier_items ti
USING ranking_tiers t
WHERE ti.ranking_tier_id = t.id AND t.ranking_id = $1 AND ti.ranked_item_id = $2;

-- name: InsertPlacement :exec
INSERT INTO ranking_tier_items (ranking_tier_id, ranked_item_id, position)
VALUES ($1, $2, $3);
