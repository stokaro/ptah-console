# Deployment and compatibility

## One binary

`ptah-console` is one Go binary that serves the Connect API under `/api/` and
the embedded web app everywhere else. It runs as `ptah-console local` on a
workstation, or from a container image in a cluster.

## Hosted installation

A Helm chart in this repository installs the console in a namespace of its
own, never the operator's. It renders:

- one Deployment with one replica and the Recreate strategy; more replicas are
  refused, because sessions live in memory;
- a Service, and optionally an Ingress or an HTTPRoute;
- a ServiceAccount, the exact impersonation ClusterRole and its binding;
- RoleBindings from each account's Kubernetes username to Ptah roles in the
  chosen namespaces;
- references to existing Secrets for the password file and TLS;
- a non-root, read-only Pod with all capabilities dropped.

It installs no operator, reads no operator Secret, and shares no
ServiceAccount, volume or credential with the operator. Uninstalling it, or a
console that is down, changes nothing the operator does; approvals already
admitted stay admitted.

## Supported versions

The console reads the operator's API types and plan reader as a Go module, so
each console release names the operator versions it was built and tested
against in `support/compatibility.json`, together with the Kubernetes minors
of the operator's support window. Before `v1` neither the operator API nor
the console makes a compatibility promise; pin both.

## How an action is judged supported

The console does not read the operator's Deployment or configuration to find
its version. Each decision is judged on the object it concerns:

- A schema plan carries `spec.contractVersion`. The console approves only a
  plan whose contract version its plan reader supports; any other is shown with
  the reason and the action is disabled.
- The API group, version and kinds the console uses are checked through
  discovery, which every authenticated identity may read.
- Anything the console cannot establish is shown as unknown and never as
  supported.
