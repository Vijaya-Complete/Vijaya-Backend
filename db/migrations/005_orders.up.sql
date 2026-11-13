CREATE TABLE orders (
    id                    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id               UUID REFERENCES users(id) ON DELETE SET NULL,
    issue_id              UUID NOT NULL REFERENCES issues(id) ON DELETE RESTRICT,
    buyer_name            VARCHAR(120) NOT NULL,
    buyer_email           VARCHAR(255) NOT NULL,
    buyer_phone           VARCHAR(30),
    format                order_format NOT NULL,
    copies                SMALLINT NOT NULL DEFAULT 1 CHECK (copies BETWEEN 1 AND 10),
    delivery_zone         delivery_zone,
    delivery_address_line1 VARCHAR(200),
    delivery_address_line2 VARCHAR(200),
    delivery_city         VARCHAR(100),
    delivery_district     VARCHAR(100),
    delivery_fee          INTEGER NOT NULL DEFAULT 0,
    unit_price            INTEGER NOT NULL,
    subtotal              INTEGER NOT NULL,
    total                 INTEGER NOT NULL,
    payment_method        payment_method NOT NULL,
    status                order_status NOT NULL DEFAULT 'pending',
    paid_at               TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_orders_issue   ON orders(issue_id);
CREATE INDEX idx_orders_status  ON orders(status);
CREATE INDEX idx_orders_created ON orders(created_at DESC);
CREATE INDEX idx_orders_user    ON orders(user_id) WHERE user_id IS NOT NULL;

COMMENT ON COLUMN orders.unit_price   IS 'Rs. in cents (e.g. 35000 = Rs. 350)';
COMMENT ON COLUMN orders.delivery_fee IS 'Rs. 300 Colombo / Rs. 500 outside; 0 for PDF';
