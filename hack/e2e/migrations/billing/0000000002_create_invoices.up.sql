CREATE TABLE invoices (id bigint PRIMARY KEY, customer_id bigint NOT NULL REFERENCES customers (id), total_cents bigint NOT NULL);
