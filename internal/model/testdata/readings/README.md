# Operator readings

Status documents the operator itself wrote during its acceptance runs, copied
from stokaro/ptah-operator `testdata/e2e/readings` at commit
9571884c9859e318bd03e75ea316a0c2c9bdc00b. The model is tested against what the
operator produced, not against statuses written by hand.

| File | What it is |
| --- | --- |
| `alert-schema-recovered.json` | A PtahSchema waiting for an approval after drift. |
| `alert-schema-recovery-wait.json` | A suspended PtahSchema whose last Apply ended with an unknown outcome. |
| `partial-run-left-a-dirty-revision.json` | A PtahMigration status blocked on a dirty revision row. |
| `isolated-node-durable-migration.json` | A PtahMigration status whose history matches the artifact. |
