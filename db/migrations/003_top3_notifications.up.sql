CREATE TYPE notification_type AS ENUM (
    'top30_selected','top3_confirmed','admin_edited','admin_deleted',
    'new_congratulation','new_review','order_confirmed'
);

CREATE TABLE top3_contributors (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    issue_id            UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    user_id             UUID NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    age_group           age_group NOT NULL,
    rank                SMALLINT NOT NULL CHECK (rank BETWEEN 1 AND 3),
    aggregate_score     NUMERIC(4,2) NOT NULL DEFAULT 0,
    suggested_by_system BOOLEAN NOT NULL DEFAULT TRUE,
    confirmed_by_admin  BOOLEAN NOT NULL DEFAULT FALSE,
    confirmed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, age_group, rank)
);

CREATE INDEX idx_top3_issue ON top3_contributors(issue_id);

CREATE TABLE congratulations (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    top3_id        UUID NOT NULL REFERENCES top3_contributors(id) ON DELETE CASCADE,
    sender_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body           VARCHAR(280) NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_congrats_top3 ON congratulations(top3_id);

CREATE TABLE congratulation_replies (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    congratulation_id UUID NOT NULL REFERENCES congratulations(id) ON DELETE CASCADE,
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body              VARCHAR(280) NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE notifications (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       notification_type NOT NULL,
    message_en VARCHAR(300) NOT NULL,
    message_si VARCHAR(300) NOT NULL,
    message_ta VARCHAR(300) NOT NULL,
    related_id UUID,
    read       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notifications_user    ON notifications(user_id, read);
CREATE INDEX idx_notifications_created ON notifications(created_at DESC);
