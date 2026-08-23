-- +goose Up
CREATE TABLE rankings (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid        uuid        NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    name        text        NOT NULL DEFAULT 'Untitled ranking',
    description text        NOT NULL DEFAULT '',
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX rankings_user_id_idx ON rankings (user_id);

-- +goose Down
DROP TABLE rankings;
