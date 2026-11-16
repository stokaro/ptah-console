# Operator readings

Status documents the operator itself wrote during its acceptance runs. The
model is tested against what the operator produced, not against statuses
written by hand. The two `alert-schema-*` readings are copied from
stokaro/ptah-operator `testdata/e2e/readings` at commit
9571884c9859e318bd03e75ea316a0c2c9bdc00b; where the others come from follows
the table.

| File | What it is |
| --- | --- |
| `alert-schema-recovered.json` | A PtahSchema waiting for an approval after drift. |
| `alert-schema-recovery-wait.json` | A suspended PtahSchema whose last Apply ended with an unknown outcome. |
| `partial-run-left-a-dirty-revision.json` | A PtahMigration blocked on a dirty revision row, its partial run held as unresolved, read while a Resolve runs. |
| `isolated-node-durable-migration.json` | A PtahMigration whose history matches the artifact after a run its policy allowed. |
| `migration-awaiting-approval.json` | The PtahMigration `deliveries` waiting for an approval, its interval read just ended. |
| `migration-reading-again.json` | The same migration during that read: phase `Reading`, a History operation running, the plan still current and approval still required. |
| `migration-between-read-operations.json` | The same read between its last two operations: phase `Planning`, nothing running. |
| `uncertain-migration-retained-claim.json` | Two PtahMigration statuses, MySQL and PostgreSQL, `Blocked` with reason `ApplyOutcomeUnknown` on a run nobody accounted for. |

The three `deliveries` readings were recorded by watching the object in the
console's acceptance lab on October 24, 2026: Kubernetes 1.37.0, operator
`70bd7654e4a8a694ef8ab41e769535f456263c4b`, Ptah v0.15.0 executor. They are
whole objects without `managedFields` and the client-side apply annotation, and
name Secrets only by name and key.

`uncertain-migration-retained-claim.json` is copied unchanged from the
operator's `testdata/e2e/readings` at the tag v0.2.9; it is the same at v0.2.14,
which the console requires. The operator recorded it in CI run 36675116019, and
`uncertain-migration-retained-claim.md` beside it there says which fields it
keeps.

`partial-run-left-a-dirty-revision.json` and
`isolated-node-durable-migration.json` are copied unchanged from the operator's
`testdata/e2e/readings` at the tag v0.2.14. The operator recorded them again in
stokaro/ptah-operator#1040, on Kubernetes 1.36.4 with the Ptah v0.23.0
executor, in place of the copies from commit 9571884c; its `README.md` there
says which rows printed them.
