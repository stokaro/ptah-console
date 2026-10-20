CREATE TABLE refunds (id bigint PRIMARY KEY, payment_id bigint NOT NULL REFERENCES payments (id), amount_cents bigint NOT NULL);
