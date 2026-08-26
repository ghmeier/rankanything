-- +goose Up
ALTER TABLE ranking_items ALTER COLUMN title DROP NOT NULL;

-- +goose Down
ALTER TABLE ranking_items ALTER COLUMN title SET NOT NULL;
