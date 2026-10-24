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

## Full console: navigation

| Shows | Reads | Verbs |
| --- | --- | --- |
| How many resources need attention, how many the console reads | `ptahschemas`, `ptahmigrations` | list, watch |
| Whether the operator keeps the schedule its resources promise | `ptahschemas`, `ptahmigrations` | list, watch |
| Who the console acts as | `selfsubjectreviews` | create |

The sidebar reads nothing the resource list does not. The console does not
read the operator's Deployment or Pods, so the operator's health is judged by
the evidence the resources carry: each one names in
`status.nextReconciliationTime` when the operator will read it again, and a
resource more than five minutes past that time counts as late. A suspended
resource, one with an operation in progress and one that promises no read are
left out of the judgment, and with nothing left to judge the sidebar says so
rather than reporting the schedule as kept. The version beside it is the
operator module the console was built against, recorded by the build.

The accent a viewer picks colors actions, links and focus. It is kept in the
browser, and the states keep their own colors whatever the accent is.

## Full console: architecture and safety

The page reads nothing from the cluster. It describes how the operator works
and what it holds itself to, and links to the operator's own documentation for
each part.

## Full console: overview

| Shows | Reads | Verbs |
| --- | --- | --- |
| How many resources are in each state, what needs a person, namespace health | `ptahschemas`, `ptahmigrations` | list, watch |
| What moved, plans and executions per day, recent activity | the same resources' `status.journal`, and the decisions | list, watch |

The overview reads nothing the resource list and the decision list do not.
"Needs attention" takes each resource's leading attention, the one the server
lists first: an unknown outcome before a failure or a block, those before a
plan waiting for approval, then drift and stale status, and within a kind the
longest wait first. A suspended resource is counted as suspended, not as
needing attention.

The tiles count the states the resources are in now. Under three of them the
journals say what moved: failures, Applies and runs, and verifications that
found the database in sync, over the last 24 hours or since the journals are
complete, whichever is shorter, and the tile says which. "Plans and
executions" counts plans published and Applies and runs that ended, per UTC
day, over the days the journals cover in full and at most a week; today counts
from midnight, or from when the journals became complete if that was today,
and the page says so. "Recent activity" is the newest entries of the activity
feed. Where no journal is kept, as under an operator older than 0.2.7, it is
the newest `lastTransitionTime` of the conditions each resource keeps, which
is not a history, and the page says that instead. Namespace health has a
search and shows eight namespaces until asked for all.

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
| The runs the journal keeps, how each ended and how long it took | the migration's `status.journal` | get |

The tiles above the page are the history's own numbers (current version and
checkpoint, applied, pending) and the runs the journal keeps, with how many
failed since it is complete. The runs list is newest first and narrows by how
a run ended; its chart draws one bar per run that recorded its start, because
the operator records a run, not each migration in it.

The operator publishes a summary of the revision table, not its rows: the
current version and checkpoint, how many migrations are applied and pending,
whether a row is dirty, and the versions that were modified or arrived out of
order. The page lists the versions the last run applied, the plan's files and
the refused versions, and says that the earlier versions are a count. The
next history read is `status.nextReconciliationTime`; there is no "read now",
because the operator has no such action, and no retry, rollback or force.

## Full console: plan review

| Shows | Reads | Verbs |
| --- | --- | --- |
| What the plan changes, from its manifest | `ptahschemaplans` or `ptahmigrationplans` | get |
| The exact SQL with the plan's own marks, and whether its chunks checked out | `ptahschemaplanchunks`, through the operator's plan reader | get |
| Whether this identity may approve here | `selfsubjectaccessreviews` | create |
| A plan a link named that is no longer current | the resource's `status.journal`, and the decisions | get, list |

The page is the focused view's plan review in the full console, with what
an approver weighs beside it. Statement types count the marks Ptah gave each
statement, its severity and reason; the console parses no SQL. Integrity says
what the plan reader established: every chunk checked by size, digest and
order before any SQL is shown, the manifest alone for an identity that may not
read the chunks, and nothing of a plan that failed a check. The SQL can be
filtered, or narrowed to the marked statements, and keeps the plan's
numbering. The approval panel lists what the console checks before it offers
to send one: the plan read is the current one, its contract is one the console
reads, for a schema plan, and RBAC lets this identity create the approval;
admission and the operator decide again either way. A link that names a plan
by `?plan=` says when that plan is no longer current, what replaced it, and
what the journal and the decisions record of it, and approves nothing from it.

## Full console: execution and diagnosis

| Shows | Reads | Verbs |
| --- | --- | --- |
| The operation running now, its attempt and how long it has run; the last outcome; when the database is read next | the resource's status and `status.journal` | get |
| Every Job the status names, by its own conditions, and its first failed container | `jobs`, `pods`, as on call | get, list |
| A log excerpt, asked for only by an identity RBAC lets read logs | `pods/log` | get |
| What the database was last found to be: a fresh read still due, drift from the last read, or the migration history | the resource's status | get |
| The objects the status names, by name: plans, the approval, Jobs, the source, the Secret by its key, the realm | the resource's status | get |
| Failure causes by operation and reason, and runs by where they stopped | the resource's `status.journal` | get |
| Events about the resource | `events` for its UID | list with a field selector |

The page reads the evidence the history page reads and answers a narrower
question: what runs now and what a person may do about it. Its next steps are
the attention the status raises, the plan review while a plan waits, and the
acknowledgment of a run nobody can settle; it offers no retry, rollback or
force, because the operator has none. The Secret the target names stays a
name and a key. Failure causes count what the journal keeps and say from when
it is complete; a resource whose operator keeps no journal shows none.

## Full console: execution and history

| Shows | Reads | Verbs |
| --- | --- | --- |
| The controller's conditions and Events, the claimed operation and the next reconciliation | the resource's status, `events` for its UID | get; list with a field selector |
| The Jobs status names, their Pods, containers and log excerpts | `jobs`, `pods`, `events`, `pods/log`, as on call | get, list, get |
| What status records about the database: the applied plan, the last read, a fresh read due, the history and the last run | the resource's status | get |
| The resource's journal: plans, approvals, Applies and runs with how long they took, verifications, acknowledgments and failed attempts | the resource's `status.journal` | get |

The three lanes are the evidence the on-call page reads, laid out by who
left it, and the journal's events: what the controller decided on the
controller lane, what reached the database on the database lane. A run the
journal records is listed once, from the journal, with its start. Entries with a time come oldest first; a container's end keeps no
time of its own and follows them; evidence that is gone stays as an entry
that says why: not retained, not granted or unreadable. The original error is
pinned above the lanes: a failed run's own message for a migration, with
where it stopped when the run recorded it, the failure condition otherwise,
and failing those the first container that exited with an error. The page
says from when the journal is complete, or that none is kept.

## Full console: activity

| Shows | Reads | Verbs |
| --- | --- | --- |
| Plans, Applies and runs, verifications and failures | `ptahschemas`, `ptahmigrations`, their `status.journal` | list, watch |
| Approvals and acknowledgments, with who decided | `ptahschemaapprovals`, `ptahmigrationapprovals`, `ptahmigrationrunacknowledgments` | list |
| Kubernetes Events about the operator's objects | `events`, selected by `involvedObject.apiVersion=operator.ptah.run/v1alpha1` | list with a field selector |

Besides the Events, the feed reads nothing the resource list and the
decision list do not. The API server keeps Events for an hour by default, so
they reach back less far than the journals; the API server selects them by
the involved object's API version, so a Job's or a Pod's are not among them
and stay with the diagnosis of the resource that names the Job. An Event
about an approval names the approval and links nothing, since the Event does
not say whose it is. Each namespace's list keeps its newest 100, and the page
says where it kept fewer than there are, and where the identity may not list
Events at all. Each
resource's journal is the operator's own bounded record of its lifecycle:
significant events only, at most 64 of them, none more than 30 days older
than the newest. The page says from when the journals it read are complete,
the latest `coveredSince` among them, and how far back the earliest reaches,
and it promises no period beyond that. A decision the console can list stands
for the journal's acceptance or acknowledgment entry it led to, so an approval
appears once, with who made it as admission recorded; one it cannot list
leaves the journal's entry in place. A failed run names the migration, the
statement's position and the SQLSTATE or error number, never the engine's
message. The type, period and person filters live in the address. A resource
whose operator keeps no journal is counted and named as such rather than
shown as quiet.

## Full console: pre-flight

| Shows | Reads | Verbs |
| --- | --- | --- |
| What the identity may do in each namespace | `selfsubjectaccessreviews` | create, one per question |
| Whether the cluster serves the operator's group version and the kinds the console uses | discovery | get |
| The cluster's Kubernetes version | discovery, `/version` | get |
| The operator module the console was built against, the plan contracts it reads, and the Kubernetes minors its release was tested on | nothing; the build and `support/compatibility.json` record them | none |

The checks are worked out in the browser from what the other rows already
read: the operator's API, the cluster's version against the tested minors,
the session, how many offered namespaces are readable and whether the list was
cut, the live watch, and the operator's schedule as the sidebar judges it.
"Re-run checks" asks every question again. The matrix has a row per namespace
and a column per question, the thirteen questions every page already asks,
grouped as read, decide, change and diagnose; it shows 25 namespaces at a time
and a filter narrows them, and a legend gives each question in the API
server's words. An answer the server could not establish reads as unknown and
counts as not allowed. "Copy kubectl command" copies the `kubectl auth can-i
--list` loop that asks the same of the namespaces shown, through the
kubeconfig context in local mode and with `--as` for the account a hosted
console impersonates, which needs the right to impersonate it. The page also
says what the console never reads, whatever RBAC allows.

## Full console: command palette

The palette reads nothing. It searches the console's own pages and the
schemas and migrations the pages already listed, by name, namespace and
state, and moves the browser to the one chosen. ⌘K or Ctrl+K opens it
anywhere, and `/` outside a field.

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

| Shows | Reads | Verbs |
| --- | --- | --- |
| The namespaces the console offers, and what the identity may do in each | `namespaces`, only when none is named; `selfsubjectaccessreviews` | list; create, one per question |

The console never needs to list namespaces. In local mode the person names
them (`--namespace`), or the context's namespace is offered; in hosted mode
the installation configures a ceiling, and an agent's cluster has its own.
Only when none of these names a namespace are the namespaces the identity may
list offered. The console offers at most 64: a configuration that names more
is refused when it is read, and a list longer than that is cut to its first 64
names and said to be cut, with how to name the namespaces instead. A namespace
the identity cannot read is shown as "no access", never as zero resources.

In the full console the viewer chooses which of the readable namespaces the
pages read, from the top bar or, on a first visit with more than one readable
namespace, from "Choose where you want to work". The choice is kept in the
browser, apart for each cluster and each identity; it filters what the
console reads and grants nothing. Choosing every readable namespace keeps
every one, including a namespace granted later. A choice none of whose
namespaces is readable any more shows every readable namespace, and says so.
The focused view reads every readable namespace whatever the full console was
narrowed to, so nothing waiting for a person is hidden there by a choice made
elsewhere.
