# Where the design and the contract differ

The screens follow the Claude Design project "K8s Operator UI Design": the
"Ptah Focus" role views first, then the full console ("Ptah Console v6"). Where
a mock-up shows something stokaro/ptah-operator#588 rules out for this phase,
the console does the following instead.

| The design shows | The console does |
| --- | --- |
| "as oidc:alex@acme.com" | The identity requests run as: the kubeconfig's user in local mode, `ptah-console:<installation>:<account>` with the display name in hosted mode. OIDC sign-in is a later, optional profile. |
| The Admin role: connections, Add cluster, Quarantine | Not in the first phase. External clusters arrive with the outbound agent in phase 2. |
| "prod-ap-2 disconnected · outbound connector" | Phase 2. Phase 1 shows only a lost watch on the one API server. |
| "Sign in to prod-eu-1" | A per-cluster sign-in belongs to the native-credentials profile, phase 3. |
| A "Prototype outcome" switcher | Prototype only. |
| Edit YAML, seven checks, server dry-run | No generic edit: #588 forbids forwarding an arbitrary patch. A read-only YAML view and "Copy as patch", done in the browser and sent nowhere. |
| Create a PtahSchema from a template | Authoring is a later step. |
| "Plans & executions, 7 days", "Failure causes, 30 days" | Not shown. Kubernetes keeps Events for about an hour, and #588 rules out synthetic history. |
| Overview tiles with 7-day deltas and sparklines, a "Last 7 days" range, a "Kubernetes resources" panel | The tiles count the states the resources are in now and link to the list filtered to each. Nothing records how those counts moved, so no delta or trend is drawn. |
| "Re-read history (interval)" as an action | The operator has no such API. The console shows when the next read is due, from `status.nextReconciliationTime`. |
| Fonts from Google Fonts | Bundled from npm packages, so the page loads nothing from another origin. |
| SQL lines tagged "drops data", "changes access" | Tags come from the plan's own statement severity and reason, recorded by Ptah, and the plan-level findings. The console does not parse SQL to invent them. |
