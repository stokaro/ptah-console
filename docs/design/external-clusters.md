# External clusters

Phase 2 lets one hosted console reach Ptah resources in clusters other than
the one it runs in. stokaro/ptah-operator#588 sections 11 to 14 are the
contract; this document settles what they leave to the implementation, and
each phase 2 issue (#95 to #103) implements a section of it. Nothing in phase 2
is built before the owner has reviewed it; the decisions at the end were taken
in that review.

## What stays the same

The browser talks only to the hosted console, with the session it has today.
A person's request still runs as one exact Kubernetes username, and the API
server and the operator's admission still decide. "This cluster" keeps the
co-located path of phase 1; an agent is added only for a cluster the console
does not run in.

```text
browser ── session, Connect ──> ptah-console ──(This cluster)── impersonating client ──> kube-apiserver
                                     │
                                     │  agent channel: TLS, opened by the agent
                                     v
                            ptah-console-agent (target cluster) ── impersonating client ──> kube-apiserver
```

## The channel

The agent dials the console; the target cluster accepts no inbound connection
for it. The console serves agents on a listener of their own, on its own port
and its own Service, separate from the browser's, so a browser session can
never reach an agent endpoint and an agent credential can never reach a
browser endpoint. The two never share a TLS name or a route. How the agent
Service is exposed, through an Ingress or a LoadBalancer, is the installer's
choice in the chart.

- **Transport.** HTTP/2 over TLS 1.3, with one long-lived Connect
  bidirectional stream per connection: the console sends typed requests down
  it and the agent sends the results back. Connect is what the browser API
  already uses, so the contract is one more service in `proto/`, generated and
  checked like the others, and no custom framing or cryptography is written.
- **The agent authenticates the console** by TLS: the console's own CA and the
  server name it was enrolled with. The system roots are never used, so a
  certificate a public CA issued for the same name is refused. A certificate
  from another CA, or for another name, ends the attempt before any credential
  is sent.
- **The console authenticates the agent** by the connection's credential, sent
  as a bearer token on the stream's request. The console stores a verifier only:
  the SHA-256 of the 256-bit credential, compared in constant time. A
  high-entropy random secret needs no slow hash, and a verifier cannot be
  presented in place of the credential.
- **Binding.** The stream is bound at open to one connection UID and its
  generation. Every frame repeats both; a frame naming another connection, or
  an older generation, closes the stream.

## Typed requests

The console sends only the operations phase 1 already performs, and the agent
serves no other:

| Request | What the agent does |
| --- | --- |
| Capabilities | `SelfSubjectAccessReview` and `SelfSubjectReview` as the mapped user |
| List and get schemas, migrations, plans and decisions | reads through the same typed client and `pkg/planread` as phase 1 |
| Diagnostics | the Jobs, Pods, Events and log excerpts phase 1 reads, by the references status names |
| Watch window | one watch of at most 30 seconds, as the mapped user |
| Approve, acknowledge, suspend | the same writes as phase 1, as the mapped user |

Each request carries an envelope: request ID, console instance, the console
session's generation, the immutable account ID and a deadline. Every frame
carries the connection UID and generation, and the action and the namespaces
it names are the typed request itself, the message the console's own service
takes. The agent maps the account ID through
its own allowlist to one Kubernetes username and impersonates exactly that
name, with no groups, UID or extras. The allowlist is in the agent chart's
values and nowhere else, so a change to it is a Helm upgrade or a GitOps
change someone reviews; the agent reloads it without a restart. An account the allowlist does not name, a
namespace outside its ceiling, or an action outside the table is refused before
any call. A username, group or header the console might send is never read.

The agent never reads Secrets or `PtahResultRecord` objects, never writes RBAC,
and calls nothing but its own cluster's kube-apiserver. It is not a proxy: no
URL, path or verb the console chose reaches the API server.

## Routing

A page names the cluster it shows by connection UID, in the `Ptah-Connection`
header of every request; no header, or this cluster's UID, is served by the
console as before. The console finds the entry with its own identity and
reads it as the person, so a connection the person was not granted reads as
one that does not exist. The session and the console's own information are
the console's whichever cluster a page shows; every other procedure follows the
header, and a test holds that list to the API.

A live view of an external cluster is a sequence of the agent's watch
windows. A change between two windows could pass unreported, so each window
after the first opens by saying that anything it watches may have changed, and
the page reads it again: an external cluster's view is at most one window
behind.

## Bounds and failures

- A frame is at most the plan size `pkg/planread` accepts plus its envelope;
  anything larger closes the stream.
- At most 64 requests are in flight per connection and 8 per account; past
  that, the console answers "busy" without sending.
- A request carries a deadline of at most 30 seconds, and the agent enforces
  it on its own clock.
- A decision whose stream drops before its answer arrives is reported as
  unknown, exactly like a lost answer in phase 1: the console reads the decision
  back by its name once the channel returns, and never sends it again.

## Enrollment

An installation administrator starts an enrollment in the console: the
cluster's name, the namespace ceiling, and the accounts the agent will act
for with their roles there. The console creates a `ConsoleClusterEnrollment`
as that administrator, so RBAC decides who may, and records in its status the
verifier of a 256-bit bootstrap secret valid for 10 minutes, and the
enrollment's fingerprint. The administrator gets an enrollment file with the
secret, once; the console keeps the verifier only. The console holds at most
16 enrollments, and one cannot take the name of a connection or of another
enrollment.

On the target, the administrator's `ptah-console cluster enroll` reads the
file with their own kubeconfig, shows the fingerprint the console showed,
makes the channel credential there, writes it into the agent's Secret, and
prints the exact Helm command that installs the agent with its RoleBindings.
It then completes the enrollment on the console's agent listener, over TLS
that trusts only the console's CA, with the bootstrap secret, the target's
API server as the kubeconfig names it, and the credential's verifier. The
credential never leaves the target. The secret completes one enrollment once:
the same report sent again is taken again, so a helper that lost the answer
can retry, and any other report is refused. An enrollment nobody completed
within 10 minutes, or nobody activated within a day of completion, is
removed.

## Credentials over time

The credential expires; 90 days by default. The console warns from 14 days
before. Rotation is a second credential that both sides accept for at most one
hour: the target administrator writes it into the agent's Secret, the agent
reloads it and reconnects, and the console retires the old one only after a
stream authenticated with the new one. The agent has no permission to write its
own Secret.

The verifiers are in the status of the connection's `ConsoleCluster`, at most
two, which only the console's own identity may write: a connection
administrator manages entries and cannot write their status. Each verifier
carries when it was issued, when it expires, when it first opened a channel,
and, once a newer one has, when it retires: an hour later or at its expiry,
whichever comes first. The status also carries the channel generation, which
starts at 1 and only rises. Raising it ends the open channel, and an agent still
bound to the old generation is refused until its installation names the new
one. The console holds one channel per connection; a newer one ends the older.

## Registry

`ConsoleCluster` holds what decides trust: the connection UID, the target's API
server identity as enrolled, the console's CA and name the agent expects, the
namespace ceiling, and the configuration owner. CEL keeps those fields
immutable; changing one is a new connection, enrolled again, and the old one is
revoked. `ConsoleClusterPreferences` holds what does not: a display name,
labels and the enabled flag, against an immutable connection UID. Quarantine is
recorded apart from both, by UID and generation, so reapplying an old record
from Git does not lift it.

"This cluster" becomes a `ConsoleCluster` of the co-located profile when the
registry is installed. It keeps the installation ID and every account ID, so no
account loses a grant and no plan or approval the operator holds changes.

## Trust disclosure

These words appear in the enrollment screen and in the installation guide:

> The agent acts in this cluster as each Console account the target
> administrator allows, with the Kubernetes rights those usernames have here.
> A compromised console can make requests as those accounts. A compromised
> agent can use those accounts' rights in this cluster. Keep the allowlist
> short and the bound roles to Ptah's resources, and remove the agent's
> impersonation binding to cut it off at once, whatever the console says.

## Decisions

The owner took these on 2026-10-21, in the review of this proposal:

| Question | Decision |
| --- | --- |
| Does the agent listener share the console's entry? | No. It has its own port and Service; an Ingress or LoadBalancer for it is the installer's choice. |
| Which certificates does the agent trust for the console? | Only the console's own CA, with the name given at enrollment. A public CA is not accepted in the first release. |
| Where is the account allowlist? | In the agent chart's values only, reloaded without a restart. No ConfigMap or custom resource edits it. |
| The channel transport | One Connect bidirectional stream over HTTP/2 and TLS 1.3, opened by the agent; the console keeps a SHA-256 verifier of the credential. |
