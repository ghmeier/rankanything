-- +goose Up
CREATE TABLE ranking_invites (
    id                bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at        timestamptz NOT NULL DEFAULT now(),
    sent_at           timestamptz NOT NULL DEFAULT now(),
    expires_at        timestamptz NOT NULL,
    token             text        NOT NULL UNIQUE,
    user_id           bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    invited_user_id   bigint      REFERENCES users (id) ON DELETE CASCADE,
    invited_email     citext,
    ranking_share_id  bigint      NOT NULL REFERENCES ranking_shares (id) ON DELETE CASCADE
);

CREATE INDEX ranking_invites_ranking_share_id_idx ON ranking_invites (ranking_share_id);
CREATE INDEX ranking_invites_invited_user_id_idx ON ranking_invites (invited_user_id);

-- +goose Down
DROP TABLE ranking_invites;
