# Security policy

## Reporting a vulnerability

Send it to `ask@stokaro.com`, not to the issue tracker. An issue is public from
the moment it is filed, and a report there tells everybody who reads it how to
reproduce the problem before there is a release that fixes it.

Write `security` in the subject so it is not read as a commercial enquiry.

Include the console version and mode (`local` or hosted), the operator version,
the Kubernetes version and distribution, what you did, what the console did,
and what an attacker gains.

## What is in scope

The console acts for people. Its job is to make every request with the
identity of the person who made it, and to show only what that identity may
read. A way to break that is what this policy is for:

- a request that runs with another person's identity, with the console's own
  ServiceAccount, or with an identity the configuration does not map;
- an approval or acknowledgment for a plan or run other than the one the
  person reviewed, or one recorded under another identity;
- reading, through the console, data the person's Kubernetes identity may not
  read, or any Secret or `PtahResultRecord` at all;
- a plan shown as complete when its storage failed a check;
- another web origin, page or local process using a console session or the
  loopback listener: cross-site requests, DNS rebinding, a leaked launch
  token;
- markup or terminal control sequences from cluster data executing in the
  browser;
- a session that outlives logout, expiry or a credential change.

## What is not

- Anything the person's own Kubernetes identity already allows outside the
  console. The console does not narrow RBAC; it acts within it.
- A compromised console in hosted mode acting with the rights of the identities
  it is allowed to impersonate. That is the documented limit of classic
  impersonation; keep those identities' grants narrow.
- The operator itself. Report operator issues under
  [stokaro/ptah-operator](https://github.com/stokaro/ptah-operator/security).

## Supported versions

The newest published release. Before `v1` there are no backports.
