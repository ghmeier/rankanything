-- +goose Up
CREATE TYPE user_theme_preference AS ENUM ('system', 'light', 'dark');

-- Default 'system' preserves today's behavior (prefers-color-scheme) for
-- every existing row, so this column needs no backfill.
ALTER TABLE users ADD COLUMN theme_preference user_theme_preference NOT NULL DEFAULT 'system';

-- +goose Down
ALTER TABLE users DROP COLUMN theme_preference;
DROP TYPE user_theme_preference;
