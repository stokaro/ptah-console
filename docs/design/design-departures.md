# Where the design and the contract differ

The screens follow the Claude Design project "K8s Operator UI Design": the
"Ptah Focus" role views first, then the full console ("Ptah Console v6"). Where
a mock-up shows something stokaro/ptah-operator#588 rules out for this phase,
the console does the following instead.

| The design shows | The console does |
| --- | --- |
| The Admin role: connections, Add cluster, Quarantine | Connections, Add cluster through enrollment, disable, quarantine and removal, for a hosted console that manages external clusters. A cluster is reached only through its own outbound agent; [external clusters](external-clusters.md) says what crosses the channel. |
| "prod-ap-2 disconnected · outbound connector" | A cluster whose agent is not connected is shown as disconnected, with when it was last heard from, and its pages say it is out of reach instead of showing it empty. |
| "Sign in to prod-eu-1" | A per-cluster sign-in belongs to the native-credentials profile, phase 3. |
| A "Prototype outcome" switcher | Prototype only. |
| Desired and observed object counts, a schema comparison chart, a schema profile radar | The drift the operator records from its last read, by category and severity. The operator publishes no per-object counts, and the console does not read the database to make them. |
| Edit YAML, seven checks, server dry-run | No generic edit: #588 forbids forwarding an arbitrary patch. A read-only YAML view and "Copy as patch", done in the browser and sent nowhere. |
| Create a PtahSchema from a template | A PtahSchema or a PtahMigration from typed fields: the namespace, name, engine, how the database is coordinated, the connection Secret by name and key, the source (an OCI artifact with its verification policy, a ConfigMap file, or a Git file) and the apply policy. The console builds the object from those fields and forwards no document a person wrote; any other field is set after creation, the way every other change is. |
| "Plans & executions, 7 days", "Failure causes, 30 days" | Plans and executions per day from each resource's journal, over the days the journals cover in full and at most a week, never a week the operator did not keep. Failures are in Activity by operation and reason over the same period; no 30-day chart, because a journal keeps at most 64 events and none more than 30 days older than its newest. |
| Overview tiles with 7-day deltas and sparklines, a "Last 7 days" range, a "Kubernetes resources" panel | The tiles count the states the resources are in now and link to the list filtered to each. The journals record events, not how the counts moved, so a tile says what happened in the last 24 hours, or since the journals are complete, and draws no delta or trend. The range is the journals' own, and there is no Kubernetes resources panel: the console reads no Deployment or Pod of the operator. |
| A migration's history table with each version's checksum, time and duration, and an execution-times chart | The versions the last run applied, the plan's files and the versions the history refuses, and the runs the journal keeps with how long each took, charted per run. The operator publishes a summary of the revision table, not its rows, and no per-version timing; stokaro/ptah-operator#726 adds the newest rows. |
| The failed statement and the database's error text beside a failed migration | Where the run stopped, from its failure record: the migration, the statement's position in it and the SQLSTATE or the engine's error number. The engine's message is not kept where a reader can see it, because it can quote row data (stokaro/ptah-operator#678). |
| An execution stepper with a duration, "Failure causes (last 30 days)" | The stages from status, and how long the operation running now has run, from its own start. Failure causes come from the journal, by operation and reason and by where a run stopped, over the period the journal covers in full rather than 30 days: it keeps at most 64 events, none more than 30 days older than its newest. |
| A migration's object graph | Not shown. The page names the Job, plan and artifact it reads from status. |
| "Re-read history (interval)" as an action | The operator has no such API. The console shows when the next read is due, from `status.nextReconciliationTime`. |
| Fonts from Google Fonts | Bundled from npm packages, so the page loads nothing from another origin. |
| SQL lines tagged "drops data", "changes access" | Tags come from the plan's own statement severity and reason, recorded by Ptah, and the plan-level findings. The console does not parse SQL to invent them. |
| Each namespace's attention count in the namespace picker | What the identity may do in it. The console reads only the namespaces in scope, so it does not count what waits in the others. |
| Namespace toggles that apply at once | A selection applies when the viewer says so, and never as none, so no page reads nothing by accident. |
