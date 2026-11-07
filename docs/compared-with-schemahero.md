# Compared with SchemaHero

SchemaHero is another declarative schema operator for Kubernetes. Its review
happens in `kubectl` and in the controller's logs, and its repository has no web
interface. Ptah Console gives each person who touches a schema change a page for
their job, and every request runs under that person's Kubernetes identity.

The table comes from one recorded run of released Ptah Console 0.3.0, Ptah
Operator 0.2.9 and SchemaHero 0.26.2 on one cluster with PostgreSQL 17.11, on
November 6, 2026. We build Ptah Console, so check the
[recorded output](https://blog.ptah.run/posts/ptah-operator-vs-schemahero/).

| The job | SchemaHero 0.26.2 | Ptah Console 0.3.0 |
| --- | --- | --- |
| See what waits for a decision | `kubectl schemahero get migrations` lists migrations with their times | **Approver** lists the plans waiting, with what each one changes |
| Read what will run | `kubectl schemahero describe migration` prints the SQL and a hash, with no severity | The plan page shows the SQL verified against its digests, the severity beside each statement, and what the approval is bound to |
| Approve | The CLI writes the migration's status and records the name in `--actor`, or the local operating system account | Sends that plan by UID and fingerprint under your identity, and admission records you as the approver |
| Approve a plan that has been replaced | The old migration still runs | Nothing is approved, and the console says that a newer plan replaced it |
| Find out why a change is stuck | The migration stays `APPROVED`, with the error in the controller's log | **On call** gives the next step the operator supports and the evidence Kubernetes still keeps |
| Follow a release | The migration's times | **Developer** shows resolve, plan, approve, apply and verify, with one sentence on where it stands |
| Review past decisions | Read `status.approvedBy` on each migration | **Auditor** lists who decided what and when, and exports CSV |

The console adds no authority of its own. It installs nothing in the cluster in
local mode, it never reads Secrets, and the API server's RBAC and the operator's
admission decide what you may do. [Security model](security.md) has the details.

## Limits

The console works with Ptah Operator only, so it cannot show SchemaHero's
resources. A failed statement does not show PostgreSQL's error yet
([ptah-operator#787](https://github.com/stokaro/ptah-operator/issues/787)). The
Overview counts a `Blocked` resource as "No status"
([#161](https://github.com/stokaro/ptah-console/issues/161)).

For the comparison of the operators themselves, see
[Compared with SchemaHero](https://operator.ptah.run/edge/start/compare-schemahero/)
in the operator guide.
