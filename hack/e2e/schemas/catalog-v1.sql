-- catalog v1: the desired schema a row creates through the console's form,
-- from a ConfigMap rather than a registry.
CREATE TABLE products (
  id bigint NOT NULL PRIMARY KEY,
  title text NOT NULL,
  price_cents bigint NOT NULL DEFAULT 0
);
