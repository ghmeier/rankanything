-- name: CreateRankingItem :one
INSERT INTO ranking_items (ranking_version_id, title, image_source_url, image_upload_url, source_url)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListRankingItemsForVersion :many
SELECT * FROM ranking_items WHERE ranking_version_id = $1 ORDER BY created_at, id;

-- name: GetRankingItem :one
SELECT * FROM ranking_items WHERE id = $1;

-- name: UpdateRankingItem :one
UPDATE ranking_items
SET title = $2, image_source_url = $3, image_upload_url = $4, source_url = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteRankingItem :exec
DELETE FROM ranking_items WHERE id = $1;
