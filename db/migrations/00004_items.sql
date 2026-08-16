-- +goose Up
CREATE TABLE ranked_items (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    label            text        NOT NULL,
    sublabel         text        NOT NULL DEFAULT '',
    source_url       text        NOT NULL DEFAULT '',
    source_image_url text        NOT NULL DEFAULT '',
    image_url        text        NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- The pool of items belonging to a ranking, whether or not they sit in a tier.
CREATE TABLE ranking_items (
    ranking_id     bigint      NOT NULL REFERENCES rankings (id) ON DELETE CASCADE,
    ranked_item_id bigint      NOT NULL REFERENCES ranked_items (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (ranking_id, ranked_item_id)
);

CREATE TABLE ranking_tier_items (
    ranking_tier_id bigint      NOT NULL REFERENCES ranking_tiers (id) ON DELETE CASCADE,
    ranked_item_id  bigint      NOT NULL REFERENCES ranked_items (id) ON DELETE CASCADE,
    position        integer     NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (ranking_tier_id, ranked_item_id),
    UNIQUE (ranking_tier_id, position) DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX ranking_tier_items_item_idx ON ranking_tier_items (ranked_item_id);

-- +goose Down
DROP TABLE ranking_tier_items;
DROP TABLE ranking_items;
DROP TABLE ranked_items;
