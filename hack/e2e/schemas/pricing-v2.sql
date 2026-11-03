-- pricing v2: adds the currency. A row publishes it and points pricing at it
-- through the console.
CREATE TABLE prices (
  sku text NOT NULL PRIMARY KEY,
  amount_cents bigint NOT NULL,
  currency text NOT NULL DEFAULT 'EUR'
);
