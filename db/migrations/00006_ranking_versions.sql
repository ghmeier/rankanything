-- +goose Up
CREATE TABLE ranking_versions (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    short_uuid    text        NOT NULL,
    ranking_id    bigint      NOT NULL REFERENCES rankings (id) ON DELETE CASCADE,
    published_at  timestamptz,
    UNIQUE (ranking_id, short_uuid)
);

-- Serves "resolve the live version": most recent published, falling back to the
-- draft. ORDER BY published_at DESC NULLS LAST puts the newest publish first and
-- the (at most one) null-published_at draft last, so LIMIT 1 always picks the
-- right row in a single index scan.
CREATE INDEX ranking_versions_live_idx ON ranking_versions (ranking_id, published_at DESC NULLS LAST);

-- +goose Down
DROP TABLE ranking_versions;
