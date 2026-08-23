-- +goose Up
CREATE TYPE ranking_share_role AS ENUM ('OWNER', 'EDITOR', 'READER');

CREATE TABLE ranking_shares (
    id          bigint             GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at  timestamptz        NOT NULL DEFAULT now(),
    role        ranking_share_role NOT NULL DEFAULT 'READER',
    is_public   boolean            NOT NULL DEFAULT false,
    public_slug text,
    user_id     bigint             REFERENCES users (id) ON DELETE CASCADE,
    email       citext,
    ranking_id  bigint             NOT NULL REFERENCES rankings (id) ON DELETE CASCADE
);

CREATE INDEX ranking_shares_ranking_id_idx ON ranking_shares (ranking_id);

-- public_slug is nullable and only meaningful while is_public is true; a plain
-- UNIQUE would reject multiple revoked (null) rows, so only the non-null values
-- need to be unique.
CREATE UNIQUE INDEX ranking_shares_public_slug_idx ON ranking_shares (public_slug) WHERE public_slug IS NOT NULL;

-- Makes it easier to upsert new public shares and ensure they're unique. Adding
-- ON CONFLICT DO NOTHING means we can have one query perform an upsert and don't
-- need the get-then-create pattern.
CREATE UNIQUE INDEX ranking_shares_link_share_idx ON ranking_shares (ranking_id) WHERE user_id IS NULL AND email IS NULL;

-- +goose Down
DROP TABLE ranking_shares;
DROP TYPE ranking_share_role;
