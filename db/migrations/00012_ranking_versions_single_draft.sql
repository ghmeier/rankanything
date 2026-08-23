-- +goose Up
-- A ranking has at most one draft version with no published_at.
CREATE UNIQUE INDEX ranking_versions_one_draft_idx ON ranking_versions (ranking_id) WHERE published_at IS NULL;

-- +goose Down
DROP INDEX ranking_versions_one_draft_idx;
