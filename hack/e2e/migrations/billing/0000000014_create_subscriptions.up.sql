CREATE TABLE subscriptions (id bigint PRIMARY KEY, customer_id bigint NOT NULL REFERENCES customers (id), plan text NOT NULL);
