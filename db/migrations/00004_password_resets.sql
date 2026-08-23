-- +goose Up
CREATE TABLE password_resets (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at  timestamptz NOT NULL DEFAULT now(),
    sent_at     timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    token_hash  text        NOT NULL UNIQUE,
    is_used     boolean     NOT NULL DEFAULT false,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX password_resets_user_id_idx ON password_resets (user_id);

-- +goose Down
DROP TABLE password_resets;
