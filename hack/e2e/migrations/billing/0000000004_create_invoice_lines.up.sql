CREATE TABLE invoice_lines (id bigint PRIMARY KEY, invoice_id bigint NOT NULL REFERENCES invoices (id), amount_cents bigint NOT NULL);
