-- pricing v1: applied without an approval while the fixtures are set up.
CREATE TABLE prices (
  sku text NOT NULL PRIMARY KEY,
  amount_cents bigint NOT NULL
);
