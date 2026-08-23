-- +goose Up
-- Thse composite indexes ensure that the ranking_version_id and ranking_id
-- align for ranking tiers within a ranking version.
ALTER TABLE ranking_versions ADD CONSTRAINT ranking_versions_id_ranking_id_key UNIQUE (id, ranking_id);

-- The single-column FK on ranking_version_id not needed because of the composite one below.
ALTER TABLE ranking_tiers DROP CONSTRAINT ranking_tiers_ranking_version_id_fkey;
ALTER TABLE ranking_tiers ADD CONSTRAINT ranking_tiers_version_ranking_fkey
    FOREIGN KEY (ranking_version_id, ranking_id) REFERENCES ranking_versions (id, ranking_id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE ranking_tiers DROP CONSTRAINT ranking_tiers_version_ranking_fkey;
ALTER TABLE ranking_tiers ADD CONSTRAINT ranking_tiers_ranking_version_id_fkey
    FOREIGN KEY (ranking_version_id) REFERENCES ranking_versions (id) ON DELETE CASCADE;
ALTER TABLE ranking_versions DROP CONSTRAINT ranking_versions_id_ranking_id_key;
