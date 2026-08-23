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

-- Feeds the rankings index: one row per ranking_id, its most recently
-- published version. DISTINCT ON (ranking_id) with this ordering picks the
-- newest publish per ranking in a single scan, so the index can describe N
-- rankings' live-published state in one query rather than N.
-- name: ListLatestPublishedRankingVersionsForRankings :many
SELECT DISTINCT ON (ranking_id) *
FROM ranking_versions
WHERE ranking_id = ANY(@ranking_ids::bigint[]) AND published_at IS NOT NULL
ORDER BY ranking_id, published_at DESC;

-- Feeds the rankings index: the (at most one, per the
-- ranking_versions_one_draft_idx constraint) draft version for each
-- ranking_id, in one query rather than N.
-- name: ListDraftRankingVersionsForRankings :many
SELECT * FROM ranking_versions
WHERE ranking_id = ANY(@ranking_ids::bigint[]) AND published_at IS NULL;
