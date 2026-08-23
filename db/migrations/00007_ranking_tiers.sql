-- +goose Up
CREATE TABLE ranking_tiers (
    id                  bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    position            smallint    NOT NULL,
    ranking_version_id  bigint      NOT NULL REFERENCES ranking_versions (id) ON DELETE CASCADE,
    ranking_id          bigint      NOT NULL REFERENCES rankings (id) ON DELETE CASCADE,
    title               text        NOT NULL,
    color_hex           text        NOT NULL DEFAULT '#94a3b8',
    UNIQUE (ranking_version_id, position) DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX ranking_tiers_ranking_id_idx ON ranking_tiers (ranking_id);

-- +goose Down
DROP TABLE ranking_tiers;
