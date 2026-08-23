-- +goose Up
CREATE TABLE email_verifications (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at  timestamptz NOT NULL DEFAULT now(),
    sent_at     timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    token_hash  text        NOT NULL UNIQUE,
    is_verified boolean     NOT NULL DEFAULT false,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX email_verifications_user_id_idx ON email_verifications (user_id);

-- +goose Down
DROP TABLE email_verifications;
