-- +goose Up
CREATE TABLE rankings (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        uuid        NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    user_id     bigint      REFERENCES users (id) ON DELETE CASCADE,
    title       text        NOT NULL DEFAULT 'Untitled ranking',
    description text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX rankings_user_id_idx ON rankings (user_id);
CREATE INDEX rankings_unclaimed_idx ON rankings (created_at) WHERE user_id IS NULL;

CREATE TABLE ranking_tiers (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ranking_id     bigint      NOT NULL REFERENCES rankings (id) ON DELETE CASCADE,
    label          text        NOT NULL,
    position       integer     NOT NULL,
    color          text        NOT NULL DEFAULT '#94a3b8',
    allow_multiple boolean     NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (ranking_id, position) DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX ranking_tiers_ranking_id_idx ON ranking_tiers (ranking_id);

-- +goose Down
DROP TABLE ranking_tiers;
DROP TABLE rankings;
