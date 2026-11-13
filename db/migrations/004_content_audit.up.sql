CREATE TYPE content_key    AS ENUM ('about', 'contact', 'guidelines');
CREATE TYPE message_status AS ENUM ('new', 'replied');

CREATE TABLE magazine_layouts (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    issue_id    UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    layout_json JSONB NOT NULL DEFAULT '{}',
    published   BOOLEAN NOT NULL DEFAULT FALSE,
    pdf_url     TEXT,
    last_saved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id)
);

CREATE TABLE site_content (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    key          content_key NOT NULL,
    language     language_code NOT NULL,
    content_json JSONB NOT NULL DEFAULT '{}',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (key, language)
);

CREATE TABLE contact_messages (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(120) NOT NULL,
    email       VARCHAR(255) NOT NULL,
    message     TEXT NOT NULL,
    status      message_status NOT NULL DEFAULT 'new',
    reply_body  TEXT,
    replied_at  TIMESTAMPTZ,
    replied_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_contact_status ON contact_messages(status);

CREATE TABLE audit_log (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    admin_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action      VARCHAR(100) NOT NULL,
    target_type VARCHAR(50) NOT NULL,
    target_id   UUID,
    detail_json JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_admin   ON audit_log(admin_id);
CREATE INDEX idx_audit_created ON audit_log(created_at DESC);
