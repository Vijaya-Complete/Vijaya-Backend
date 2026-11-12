CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TYPE user_role        AS ENUM ('user', 'admin');
CREATE TYPE age_group        AS ENUM ('5-8', '9-12', '13-17');
CREATE TYPE submission_status AS ENUM ('pending', 'live', 'rejected');
CREATE TYPE submission_type  AS ENUM ('upload', 'camera', 'canvas');
CREATE TYPE content_type     AS ENUM ('editor', 'pdf');
CREATE TYPE story_tag        AS ENUM ('poem', 'short_story', 'article', 'other');
CREATE TYPE issue_status     AS ENUM ('open', 'closed', 'published');
CREATE TYPE language_code    AS ENUM ('en', 'si', 'ta');
CREATE TYPE order_format     AS ENUM ('physical', 'pdf');
CREATE TYPE order_status     AS ENUM ('pending', 'paid', 'failed', 'refunded');
CREATE TYPE payment_method   AS ENUM ('card', 'bank', 'cod');
CREATE TYPE delivery_zone    AS ENUM ('colombo', 'outside');

CREATE TABLE users (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email            VARCHAR(255) UNIQUE NOT NULL,
    password_hash    VARCHAR(255) NOT NULL,
    role             user_role NOT NULL DEFAULT 'user',
    trusted          BOOLEAN NOT NULL DEFAULT FALSE,
    active           BOOLEAN NOT NULL DEFAULT TRUE,
    language_pref    language_code NOT NULL DEFAULT 'en',
    kid_name         VARCHAR(120),
    age              SMALLINT CHECK (age IS NULL OR (age >= 4 AND age <= 18)),
    age_group        age_group,
    school_name      VARCHAR(200),
    guardian_name    VARCHAR(120),
    contact_number   VARCHAR(30),
    avatar_url       TEXT,
    bio              VARCHAR(280),
    profile_complete BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deactivated_at   TIMESTAMPTZ
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_role  ON users(role);

CREATE TABLE issues (
    id                    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name                  VARCHAR(100) NOT NULL,
    description           TEXT,
    deadline              TIMESTAMPTZ NOT NULL,
    publish_date          TIMESTAMPTZ,
    max_drawings_per_user SMALLINT NOT NULL DEFAULT 5,
    max_articles_per_user SMALLINT NOT NULL DEFAULT 5,
    status                issue_status NOT NULL DEFAULT 'open',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE drawings (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issue_id        UUID NOT NULL REFERENCES issues(id) ON DELETE RESTRICT,
    title           VARCHAR(120) NOT NULL,
    description     VARCHAR(500),
    image_url       TEXT NOT NULL,
    submission_type submission_type NOT NULL DEFAULT 'upload',
    status          submission_status NOT NULL DEFAULT 'pending',
    top_30          BOOLEAN NOT NULL DEFAULT FALSE,
    avg_rating      NUMERIC(3,2) NOT NULL DEFAULT 0,
    rating_count    INTEGER NOT NULL DEFAULT 0,
    review_count    INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_drawings_user   ON drawings(user_id);
CREATE INDEX idx_drawings_issue  ON drawings(issue_id);
CREATE INDEX idx_drawings_status ON drawings(status);
CREATE INDEX idx_drawings_top30  ON drawings(top_30) WHERE top_30 = TRUE;
CREATE INDEX idx_drawings_rating ON drawings(avg_rating DESC);

CREATE TABLE articles (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issue_id        UUID NOT NULL REFERENCES issues(id) ON DELETE RESTRICT,
    title           VARCHAR(120) NOT NULL,
    description     VARCHAR(500),
    content_type    content_type NOT NULL DEFAULT 'editor',
    content_html    TEXT,
    pdf_url         TEXT,
    tag             story_tag NOT NULL DEFAULT 'other',
    status          submission_status NOT NULL DEFAULT 'pending',
    top_30          BOOLEAN NOT NULL DEFAULT FALSE,
    avg_rating      NUMERIC(3,2) NOT NULL DEFAULT 0,
    rating_count    INTEGER NOT NULL DEFAULT 0,
    review_count    INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_articles_user   ON articles(user_id);
CREATE INDEX idx_articles_issue  ON articles(issue_id);
CREATE INDEX idx_articles_status ON articles(status);
CREATE INDEX idx_articles_top30  ON articles(top_30) WHERE top_30 = TRUE;
CREATE INDEX idx_articles_rating ON articles(avg_rating DESC);
