CREATE TABLE usage_records (id bigint PRIMARY KEY, subscription_id bigint NOT NULL REFERENCES subscriptions (id), quantity bigint NOT NULL);
