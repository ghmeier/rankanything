-- +goose Up
CREATE TABLE ranking_item_tiers (
    id               bigint    GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    position         smallint  NOT NULL,
    ranking_item_id  bigint    NOT NULL REFERENCES ranking_items (id) ON DELETE CASCADE,
    ranking_tier_id  bigint    NOT NULL REFERENCES ranking_tiers (id) ON DELETE CASCADE,
    UNIQUE (ranking_tier_id, position) DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (ranking_item_id, ranking_tier_id)
);

-- +goose Down
DROP TABLE ranking_item_tiers;
