-- inventory v1: a safe plan, applied without an approval.
CREATE TABLE items (
  id bigint NOT NULL PRIMARY KEY,
  sku text NOT NULL UNIQUE,
  quantity integer NOT NULL DEFAULT 0
);
