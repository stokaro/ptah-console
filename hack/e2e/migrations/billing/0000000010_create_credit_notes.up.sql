CREATE TABLE credit_notes (id bigint PRIMARY KEY, invoice_id bigint NOT NULL REFERENCES invoices (id), amount_cents bigint NOT NULL);
