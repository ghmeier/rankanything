-- Performantly create unique public share sicne wecan rely on the constraint
-- to do nothing if a share already exists.
-- name: UpsertRankingLinkShare :one
INSERT INTO ranking_shares (ranking_id, is_public, public_slug)
VALUES ($1, true, $2)
ON CONFLICT (ranking_id) WHERE user_id IS NULL AND email IS NULL
DO UPDATE SET is_public = true, public_slug = $2
RETURNING *;

-- name: GetRankingShareByPublicSlug :one
SELECT * FROM ranking_shares WHERE public_slug = $1;

-- name: ListRankingSharesForRanking :many
SELECT * FROM ranking_shares WHERE ranking_id = $1 ORDER BY created_at;

-- name: ClearRankingPublicSlug :exec
UPDATE ranking_shares
SET is_public = false, public_slug = NULL
WHERE ranking_id = $1 AND user_id IS NULL AND email IS NULL;
