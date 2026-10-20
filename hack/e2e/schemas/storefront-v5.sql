-- storefront v5: v4, and a note on each order. Published while somebody
-- reviews v4, so the plan they read is replaced under them.
CREATE TABLE customers (
  id bigint NOT NULL PRIMARY KEY,
  signed_up_at timestamptz
);

CREATE TABLE orders (
  id bigint NOT NULL PRIMARY KEY,
  customer_id bigint NOT NULL REFERENCES customers (id),
  total_cents bigint NOT NULL,
  placed_at timestamptz,
  note text
);

GRANT SELECT ON TABLE orders TO PUBLIC;
