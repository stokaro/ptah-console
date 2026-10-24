# Using the console

The console shows what the operator is doing with each `PtahSchema` and
`PtahMigration` you may read, why, and which step is valid next. Every read
and every decision goes to the API server as you, so what a page offers is
what RBAC lets you do, and the operator's admission still decides. This page
goes through the tasks people come to it for. [Run the console on your
workstation](../install/local.md) and [Install a hosted
console](../install/hosted.md) say how to start it.

## Two ways to look

The console opens on **Focus**, four views of the same resources for four
jobs: Approver, Developer, On call and Auditor. It picks the one your identity
can act in, and the tabs at the top switch between them; a tab is a view, not
a permission.

**Full console**, one link away, shows everything at once. Its sidebar has the
Overview and the Resources list, then Activity, Architecture & safety and
Pre-flight. ⌘K, Ctrl+K or `/` opens a palette that jumps to any page or
resource by name. The namespaces the full console reads are chosen from the
top bar; a namespace your identity may not read is listed as not readable,
never as empty.

Pages follow the cluster live. If the console loses the API server, a banner
says since when, the pages keep the last state they read with its age, and
every action is off until the watch is back.

## Approve a plan

Open **Approver** in Focus, or the plan from a resource's page in the full
console. A plan shows what it changes as the plan itself reports it
(destructive statements, privilege changes), the exact SQL read through the
operator's integrity-checked plan reader, and what the approval binds: the
resource, the plan, their UIDs and the plan's fingerprint.

**Approve this plan…** asks you to confirm that you reviewed what it changes,
then sends that plan and nothing else. The answer is one of:

- **Approval admitted**: admission recorded you as the approver. Approved is
  not applied; **Follow it** opens the resource while the operator applies the
  plan and verifies it by reading the database again.
- **It changed**: a newer plan replaced the one you reviewed, and nothing was
  approved. Review the new one.
- **Refused**: admission said no, in its own words. The usual reason is the
  four-eyes control: the person who wrote the change may not approve it.
- **Kubernetes does not let you**: RBAC refused the request.

Each interval the operator reads a resource that waits for approval again.
The plan stays current while it does, but admission refuses an approval until
the read ends, so the console offers none: the page says the operator is
reading the resource and offers the approval again when the read ends.

If you may read the plan but not the chunks its SQL is stored in, the page
says so and shows the plan's manifest without SQL. A migration plan lists its
steps in order with their checksums; the operator API carries no SQL for
them.

## Follow a release

**Developer** in Focus lists each schema and migration as steps (resolve,
plan, approve, apply, verify) with one sentence on where it stands. A
schema's page in the full console starts with its current plan, the drift its
last read found, when a plan last reached the database and when the next read
is due, then why it stands where it stands and, when a GitOps controller
manages it, that a spec change made here is set back at the next sync.

A migration's page has four panels. **History** holds the last run, what you
may do next, and the migration timeline: the sequence newest first, searchable
by version or file name, with a filter by state and **Jump to failed**. Below
it are the revision table as the database records it, with each row's
checksum, write time and duration, and how long each migration ran.
**Events**, **Configuration** and **Related** hold the Kubernetes Events, the
conditions and spec, and the objects the status names.

## Find out why something failed

**On call** lists what needs a person: waiting approvals, failures, blocked
histories, outcomes nobody can prove. Each names the next step the operator
supports. **Execution & diagnosis** on a resource's page shows the Jobs its
status names, their Pods and the end of their logs as plain text, the
database as last observed, the Events, and the failures the journal counted.
**Execution and history** lays the controller, the Job and the database on
lanes, with the original error pinned.

A failed migration says where it stopped: the migration, the statement's
position and the database's SQLSTATE or error number. The database's error
text is not shown, because it can quote row data.

The console offers no retry, rollback or force: the operator has none. A
failed migration is fixed in the artifact or the database, and the next read
of the history picks it up.

## Settle a run nobody can prove

When a run ended without proof of what it applied, such as a MySQL migration
that committed part of its statements or an Apply whose Job was deleted, the
operator starts nothing more until a person settles it. Read the database's
revision table first, repair what the run left, and then **Acknowledge this
run…** on the migration's page or in On call. The operator then reads the
history again and plans from what it finds. Acknowledging needs the right to
create `ptahmigrationrunacknowledgments`.

## Create a schema or a migration

**New schema** and **New migration** on the Resources list, or in the palette,
open a form of typed fields: the namespace, the name, the engine, the database
by coordination key or realm, the connection Secret by name and key, the
source and the apply policy. The source is an OCI artifact with its
verification policy, files of a ConfigMap (one for a schema, the directory's
files for a migration), or a Git path with the CA or the known hosts its fetch
trusts.

**Check with the API server** sends a server-side dry run: the CRD and the
operator's admission judge the object and fill in its defaults, and nothing
is stored. Only the draft that check accepted can be created, as you. Any
field the form does not offer is set afterwards, the way every other change
is.

## Ask for a read now

After someone changed the database outside the operator, such as repairing a
dirty revision row, **Read again now** in a resource's **Actions** menu asks
the operator to read it before its interval ends. The page says when the
operator started the read. It is an ask: the operator starts the read once no
operation is running, and a suspended resource answers once it is resumed.

## Suspend and resume

**Suspend…** in the **Actions** menu stops the operator from starting new
Jobs for the resource; an Apply already running is followed to its end.
Only `spec.suspend` changes, but a spec change retires the current plan and
any approval of it, and the dialog says so. **Resume…** lets the operator read
the database again and plan before it applies anything.

## See what you may do

**Pre-flight** asks the API server what your identity may do in each
namespace, one question per action, and shows the answers as a matrix. It
checks that the operator's API is served, which Kubernetes minor the cluster
runs, and how your session reaches Kubernetes. **Copy kubectl command** copies
a `kubectl auth can-i --list` command that asks the same.

## Audit who decided

**Auditor** in Focus lists approvals and acknowledgments: who made each, as
admission recorded it, when, and whether it was consumed or went stale. It
exports them as CSV. **Activity** in the full console lists what the journals,
the decisions and the Kubernetes Events say happened, filtered by type,
period and person, over the period the journals cover in full.

## What the console never does

It never reads Secrets or `PtahResultRecord` objects, never sends a patch
someone wrote, and never shows a plan's SQL unless the plan reader verified
every chunk of it. A page that cannot read something says what it could not
read and why, instead of showing it empty.
