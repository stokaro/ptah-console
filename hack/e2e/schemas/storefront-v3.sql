-- storefront v3: drops the customers' email column and lets every role read
-- orders. The plan is destructive and changes privileges, so the operator
-- waits for an approval whatever spec.policy.apply says.
CREATE TABLE customers (
  id bigint NOT NULL PRIMARY KEY,
  signed_up_at timestamptz
);

CREATE TABLE orders (
  id bigint NOT NULL PRIMARY KEY,
  customer_id bigint NOT NULL REFERENCES customers (id),
  total_cents bigint NOT NULL
);

GRANT SELECT ON TABLE orders TO PUBLIC;
