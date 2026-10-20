ALTER TABLE invoices ADD COLUMN subscription_id bigint REFERENCES subscriptions (id);
