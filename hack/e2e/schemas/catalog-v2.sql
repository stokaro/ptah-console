-- catalog v2: one column more, which a person signed in with OpenID Connect
-- approves through the console.
CREATE TABLE products (
  id bigint NOT NULL PRIMARY KEY,
  title text NOT NULL,
  price_cents bigint NOT NULL DEFAULT 0,
  sku text
);
