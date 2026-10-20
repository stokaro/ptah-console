-- Holds the run open for a minute and changes nothing, so a row can take
-- the Job away while it runs. With no effect to undo, the repair is to
-- remove its unfinished revision.
SELECT pg_sleep(60);
