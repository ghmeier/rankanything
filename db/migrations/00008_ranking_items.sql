-- +goose Up
CREATE TABLE ranking_items (
    id                  bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    ranking_version_id  bigint      NOT NULL REFERENCES ranking_versions (id) ON DELETE CASCADE,
    title               text        NOT NULL,
    image_source_url    text,
    image_upload_url    text,
    source_url          text
);

CREATE INDEX ranking_items_ranking_version_id_idx ON ranking_items (ranking_version_id);

-- +goose Down
DROP TABLE ranking_items;
