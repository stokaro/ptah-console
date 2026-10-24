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
| `migration-awaiting-approval.json` | The PtahMigration `deliveries` waiting for an approval, its interval read just ended. |
| `migration-reading-again.json` | The same migration during that read: phase `Reading`, a History operation running, the plan still current and approval still required. |
| `migration-between-read-operations.json` | The same read between its last two operations: phase `Planning`, nothing running. |

The three `deliveries` readings were recorded by watching the object in the
console's acceptance lab on October 24, 2026: Kubernetes 1.37.0, operator
`70bd7654e4a8a694ef8ab41e769535f456263c4b`, Ptah v0.15.0 executor. They are
whole objects without `managedFields` and the client-side apply annotation, and
name Secrets only by name and key.
