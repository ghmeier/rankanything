-- +goose Up
ALTER TABLE ranking_versions ADD CONSTRAINT ranking_versions_short_uuid_length_check CHECK (char_length(short_uuid) = 8);

-- +goose Down
ALTER TABLE ranking_versions DROP CONSTRAINT ranking_versions_short_uuid_length_check;
