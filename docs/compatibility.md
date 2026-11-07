# Compatibility

The console reads the operator's API types and plan reader as a Go module, so
each console release is built against one operator version and tested against
that operator's lab on the newest Kubernetes minor of its support window.
[support/compatibility.json](../support/compatibility.json) is the record, and
a release refuses to build unless its row names a released operator that
`go.mod` requires.

| Console | Operator | Kubernetes |
| --- | --- | --- |
| 0.3.1 | v0.2.9 | 1.35, 1.36, 1.37 |
| 0.3.0 | v0.2.9 | 1.35, 1.36, 1.37 |
| 0.2.0 | v0.2.8 | 1.35, 1.36, 1.37 |
| 0.1.0 | v0.2.5 | 1.35, 1.36, 1.37 |

Before `v1` neither the operator's API nor the console promises compatibility
across versions. Pin both, and move them together the way this table says.

What the console checks while it runs, rather than trusting the table:

- It approves a schema plan only when its plan reader read and verified the
  plan as the person approving. A plan under another `spec.contractVersion`, or
  a document this build does not decode at the same version, is shown with the
  reason, the approve action is off, and the server refuses one sent anyway.
- It reads the API group, version and kinds it uses through discovery, which
  every authenticated identity may read, and says when the cluster does not
  serve them.
- Pre-flight compares the cluster's Kubernetes minor, as discovery reports it,
  with the minors this release was tested on, and warns about one outside
  them. It refuses nothing on that account.
- Anything it cannot establish is shown as unknown, never as supported.
