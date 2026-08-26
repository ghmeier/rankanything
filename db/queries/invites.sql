-- name: CreateRankingInvite :one
INSERT INTO ranking_invites (token, user_id, invited_email, ranking_share_id, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetRankingInviteByTokenHash :one
SELECT * FROM ranking_invites WHERE token = $1;

-- name: MarkRankingInviteRedeemed :exec
UPDATE ranking_invites SET invited_user_id = $2 WHERE id = $1;

-- name: ListRankingInvitesForShare :many
SELECT * FROM ranking_invites WHERE ranking_share_id = $1 ORDER BY created_at;
