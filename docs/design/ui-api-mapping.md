# What each screen reads

Every read and write below runs as the person, so what a screen shows is what
their Kubernetes identity may read. A screen asks the API server for a hint
first (`SelfSubjectAccessReview`) to decide what to offer; the request itself
decides. A hint that cannot be computed is shown as unavailable.

All operator kinds are in the group `operator.ptah.run`, version `v1alpha1`.

## Kinds the console never reads

Secrets, `PtahResultRecord` objects, ConfigMaps, `PtahRealm` objects, Leases
and status subresources, whatever RBAC allows. A target is shown by its Secret
reference, name and key, and never by its content. A plan's SQL is read only
from `PtahSchemaPlanChunk` objects through the operator's `pkg/planread`, never
from the ConfigMaps the operator projects for an Apply.

## Focus: approver

| Shows | Reads | Verbs |
| --- | --- | --- |
| Plans waiting for a decision | `ptahschemas`, `ptahmigrations` | list, watch |
| Earlier approvals and whether they went stale | `ptahschemaapprovals`, `ptahmigrationapprovals` | list, watch |
| A schema plan's manifest and findings | `ptahschemaplans` | get |
| A schema plan's exact SQL | `ptahschemaplanchunks` | get, one request per chunk |
| A migration plan's files and checksums | `ptahmigrationplans` | get |
| Approve | `ptahschemaapprovals`, `ptahmigrationapprovals` | create |

A migration plan names files and checksums; the operator API does not carry
their SQL, and the screen says so instead of reaching for it elsewhere.

Before it creates an approval, the server reads the owner again. If its
current plan is no longer the one the person reviewed (owner UID, plan name
and UID, fingerprint, contract version), nothing is created and the screen
shows the plan that replaced it.

The operator chart's `<release>-approver` ClusterRole grants all of the above.
The operator's `examples/approver-plan-reader-role.yaml` grants `get` only: a
reviewer bound to it can open a plan from a link, and sees no queue, because
the queue is a list. The screen says which grant is missing rather than showing
an empty queue.

## Focus: developer

| Shows | Reads | Verbs |
| --- | --- | --- |
| Releases and where each stands | `ptahschemas`, `ptahmigrations` | list, watch |
| What happened | the resource's status, `events` for its UID | get; list with a field selector |
| A link for approvers | nothing | none |

The stage rail (Resolve, Plan, Approve, Apply, Verify for a schema; Resolve,
Read history, Plan, Approve, Apply, Verify history for a migration) is derived
from status and conditions only. The two families keep their own mappings: a
schema converges, a migration advances.

## Focus: on call

| Shows | Reads | Verbs |
| --- | --- | --- |
| Incidents: failures, blocked resources, unknown outcomes, unresolved runs, stale data | `ptahschemas`, `ptahmigrations` | list, watch |
| The Job an operation ran | `jobs` named by status, checked by UID | get |
| Its Pods | `pods` with `batch.kubernetes.io/controller-uid` | list |
| Events | `events` for the resource, Job and Pod UIDs | list |
| A log excerpt | `pods/log`, at most 200 lines and 64 KiB | get |
| Acknowledge an unresolved run | `ptahmigrationrunacknowledgments` | create |

Jobs are found only through references in status, never by label scans. A Job
whose UID differs from the reference is another object under the same name and
is not shown. Evidence Kubernetes no longer keeps is marked "not retained",
which is not the same as "never happened". Logs are diagnosis, not proof: a
missing log does not change what status records.

The operator's `examples/diagnostic-reader-role.yaml` grants the reads. It does
not grant chunks, so an on-call reader sees findings and not SQL.

There is no retry, rollback or force action: the operator has none.

## Focus: auditor

| Shows | Reads | Verbs |
| --- | --- | --- |
| Approvals and acknowledgments, by whom and when | the three decision kinds | list, watch |
| The exact SQL approved, while the plan is retained | `ptahschemaplans`, `ptahschemaplanchunks` | get |

Who decided is what admission stamped (`spec.approver`, `spec.acknowledgedBy`),
never what the console sent. CSV export happens in the browser and neutralizes
cells that start with `=`, `+`, `-` or `@`.

## Full console: overview

| Shows | Reads | Verbs |
| --- | --- | --- |
| How many resources are in each state, what needs a person, namespace health, recent condition changes | `ptahschemas`, `ptahmigrations` | list, watch |

The overview reads nothing the resource list does not. "Needs attention" takes
each resource's leading attention, the one the server lists first: an unknown
outcome before a failure or a block, those before a plan waiting for approval,
then drift and stale status, and within a kind the longest wait first. A
suspended resource is counted as suspended, not as needing attention. "Recent
activity" is the newest `lastTransitionTime` of the conditions each resource
keeps; a condition keeps only its last change, so this is not a history, and
the page says so. Kubernetes Events stay on each resource's page, where they
are read for that resource alone.

## Full console: resources

| Shows | Reads | Verbs |
| --- | --- | --- |
| Every schema and migration, its state, why it waits, its lifecycle and artifact | `ptahschemas`, `ptahmigrations` | list, watch |

The list is the same read the focused view makes; filtering, sorting and
paging happen in the browser, over what the identity may list, and the filters
live in the address so a filtered list can be sent as a link. A resource is
filed under one state: suspended whatever else it waits on, then needing a
person, then in progress, then in sync when every stage is done. A namespace
the identity may not read is named above the list rather than left out.

## Full console: schema

| Shows | Reads | Verbs |
| --- | --- | --- |
| Where the schema stands and why; its current and applied plans; every condition; the artifact and its verification; the target by reference and the drift its last read found; the operation under way; the spec as YAML | `ptahschemas` | list, watch; get when opened from a link |
| Review the plan | the approver's plan review | as above |
| Suspend or resume | `ptahschemas` | patch, as below |

The spec is shown read-only, as the API server stores it; a target in it
names its Secret and key, never their content. "Copy as patch" copies a
`kubectl patch --type merge` command that sets the spec to what the page
shows, for the person to run where they choose: the console sends no patch.
The drift shown is the operator's own record of its last read, by category
and severity; the operator publishes no per-object counts, so the page draws
none.

## Full console: migration

| Shows | Reads | Verbs |
| --- | --- | --- |
| How far the history is, how the last run ended, what the current plan would apply, the run waiting for a person, every condition, the artifact, the target, the spec | `ptahmigrations` | list, watch; get when opened from a link |
| The files the plan would apply | `ptahmigrationplans` | get |
| Acknowledge an unresolved run | `ptahmigrationrunacknowledgments` | create, as on call |

The operator publishes a summary of the revision table, not its rows: the
current version and checkpoint, how many migrations are applied and pending,
whether a row is dirty, and the versions that were modified or arrived out of
order. The page lists the versions the last run applied, the plan's files and
the refused versions, and says that the earlier versions are a count. The
next history read is `status.nextReconciliationTime`; there is no "read now",
because the operator has no such action, and no retry, rollback or force.

## Suspend and resume

| Does | Writes | Verbs |
| --- | --- | --- |
| Suspend or resume a schema or migration | `ptahschemas`, `ptahmigrations` | patch |

The JSON patch tests `metadata.resourceVersion` against the version the page
showed and sets `/spec/suspend`, nothing else; the API server records the
console as the manager of that one field. A resource version is the store's
revision, so an object deleted and created again under the name never carries
the version the person saw. Before the person confirms, the screen says that
a spec change retires the current plan and its approval, that suspending a
migration while its Apply runs leaves a run someone has to acknowledge, and
that a GitOps controller managing the object may revert the change.

## Namespaces

The console never needs to list namespaces. In local mode the person names
them (`--namespace`); in hosted mode the installation configures a ceiling. If
the identity may list namespaces, those are offered as well. A namespace the
identity cannot read is shown as "no access", never as zero resources.
