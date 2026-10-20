-- inventory v2: adds where an item is kept. A hosted account approves it.
CREATE TABLE items (
  id bigint NOT NULL PRIMARY KEY,
  sku text NOT NULL UNIQUE,
  quantity integer NOT NULL DEFAULT 0,
  location text NOT NULL DEFAULT ''
);
