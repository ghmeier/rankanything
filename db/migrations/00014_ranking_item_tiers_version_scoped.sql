-- +goose Up
-- Create composite indexes on ranking_version_id for items and ranking_tiers
-- Then reference the combined column across foreign key references to ensure
-- tiers and items always belong to othe same version.
ALTER TABLE ranking_items ADD CONSTRAINT ranking_items_id_ranking_version_id_key UNIQUE (id, ranking_version_id);
ALTER TABLE ranking_tiers ADD CONSTRAINT ranking_tiers_id_ranking_version_id_key UNIQUE (id, ranking_version_id);

-- Denormalize ranking_version_id to ranking_item_tiers to support
-- creating composite indexes.
ALTER TABLE ranking_item_tiers ADD COLUMN ranking_version_id bigint;
UPDATE ranking_item_tiers it
SET ranking_version_id = t.ranking_version_id
FROM ranking_tiers t
WHERE t.id = it.ranking_tier_id;
ALTER TABLE ranking_item_tiers ALTER COLUMN ranking_version_id SET NOT NULL;

-- Remove these constraints which are duplicative of the composite indexes created below.
ALTER TABLE ranking_item_tiers DROP CONSTRAINT ranking_item_tiers_ranking_item_id_fkey;
ALTER TABLE ranking_item_tiers DROP CONSTRAINT ranking_item_tiers_ranking_tier_id_fkey;

ALTER TABLE ranking_item_tiers ADD CONSTRAINT ranking_item_tiers_item_version_fkey
    FOREIGN KEY (ranking_item_id, ranking_version_id) REFERENCES ranking_items (id, ranking_version_id) ON DELETE CASCADE;
ALTER TABLE ranking_item_tiers ADD CONSTRAINT ranking_item_tiers_tier_version_fkey
    FOREIGN KEY (ranking_tier_id, ranking_version_id) REFERENCES ranking_tiers (id, ranking_version_id) ON DELETE CASCADE;

CREATE INDEX ranking_item_tiers_ranking_version_id_idx ON ranking_item_tiers (ranking_version_id);

-- +goose Down
DROP INDEX ranking_item_tiers_ranking_version_id_idx;
ALTER TABLE ranking_item_tiers DROP CONSTRAINT ranking_item_tiers_tier_version_fkey;
ALTER TABLE ranking_item_tiers DROP CONSTRAINT ranking_item_tiers_item_version_fkey;
ALTER TABLE ranking_item_tiers ADD CONSTRAINT ranking_item_tiers_ranking_tier_id_fkey
    FOREIGN KEY (ranking_tier_id) REFERENCES ranking_tiers (id) ON DELETE CASCADE;
ALTER TABLE ranking_item_tiers ADD CONSTRAINT ranking_item_tiers_ranking_item_id_fkey
    FOREIGN KEY (ranking_item_id) REFERENCES ranking_items (id) ON DELETE CASCADE;
ALTER TABLE ranking_item_tiers DROP COLUMN ranking_version_id;
ALTER TABLE ranking_tiers DROP CONSTRAINT ranking_tiers_id_ranking_version_id_key;
ALTER TABLE ranking_items DROP CONSTRAINT ranking_items_id_ranking_version_id_key;
