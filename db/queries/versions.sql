-- name: CreateRankingVersion :one
INSERT INTO ranking_versions (short_uuid, ranking_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetRankingVersionByShortUUID :one
SELECT * FROM ranking_versions WHERE ranking_id = $1 AND short_uuid = $2;

-- name: ListRankingVersionsForRanking :many
SELECT * FROM ranking_versions WHERE ranking_id = $1 ORDER BY created_at DESC;

-- The live ranking is the most recenly published one. If no rankings are published,
-- we use the draft.
-- name: ResolveLiveRankingVersion :one
SELECT * FROM ranking_versions
WHERE ranking_id = $1
ORDER BY published_at DESC NULLS LAST
LIMIT 1;

-- name: PublishRankingVersion :one
UPDATE ranking_versions
SET published_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;
