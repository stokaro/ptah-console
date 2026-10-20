-- Copies the old ledger into entries. The old table was never created in
-- this database, so the statement fails and the migration's transaction
-- rolls back: a failed run, with nothing of version 2 committed.
INSERT INTO entries (id, account, amount_cents)
SELECT id, account, amount_cents FROM ledger_entries;
