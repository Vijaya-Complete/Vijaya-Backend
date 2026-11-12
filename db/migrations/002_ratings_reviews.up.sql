CREATE TYPE content_kind AS ENUM ('drawing', 'article');

CREATE TABLE ratings (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    content_kind content_kind NOT NULL,
    content_id   UUID NOT NULL,
    user_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    guest_ip     INET,
    score        SMALLINT NOT NULL CHECK (score BETWEEN 1 AND 5),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rating_has_identity CHECK (user_id IS NOT NULL OR guest_ip IS NOT NULL)
);

CREATE UNIQUE INDEX idx_ratings_user_unique  ON ratings(content_kind, content_id, user_id)  WHERE user_id  IS NOT NULL;
CREATE UNIQUE INDEX idx_ratings_guest_unique ON ratings(content_kind, content_id, guest_ip) WHERE guest_ip IS NOT NULL;
CREATE INDEX idx_ratings_content ON ratings(content_kind, content_id);

CREATE TABLE reviews (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    content_kind content_kind NOT NULL,
    content_id   UUID NOT NULL,
    user_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    guest_name   VARCHAR(80),
    guest_ip     INET,
    body         VARCHAR(1000) NOT NULL,
    flagged      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT review_has_identity CHECK (user_id IS NOT NULL OR guest_ip IS NOT NULL)
);

CREATE INDEX idx_reviews_content ON reviews(content_kind, content_id);
CREATE INDEX idx_reviews_flagged ON reviews(flagged) WHERE flagged = TRUE;
CREATE UNIQUE INDEX idx_reviews_user_unique  ON reviews(content_kind, content_id, user_id)  WHERE user_id  IS NOT NULL;
CREATE UNIQUE INDEX idx_reviews_guest_unique ON reviews(content_kind, content_id, guest_ip) WHERE guest_ip IS NOT NULL;

CREATE TABLE review_replies (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    review_id  UUID NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id)   ON DELETE CASCADE,
    body       VARCHAR(500) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_review_replies_review ON review_replies(review_id);
