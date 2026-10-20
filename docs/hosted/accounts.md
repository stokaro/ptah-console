# Hosted accounts

A hosted console reads two files. The configuration says who may sign in and
which namespaces the console shows. The passwords file holds one bcrypt hash
per account. The chart mounts the first from a ConfigMap and the second from a
Secret. The server refuses to start when either file has anything wrong in it,
and it reports every problem in the file at once.

## The configuration

```yaml
apiVersion: console.ptah.run/v1alpha1
kind: HostedConfiguration
installationID: prod-eu-1
publicURL: https://console.example.com
namespaces:
  - storefront
  - billing
accounts:
  - id: alex
    login: alex@example.com
    displayName: Alex Morgan
  - id: sam
    login: sam@example.com
trustedProxies:
  - 10.244.0.0/16
```

| Field | Rule |
| --- | --- |
| `installationID` | A DNS label: 1 to 63 lowercase letters, digits and hyphens. It tells this installation's Kubernetes usernames apart from another's in the same cluster. |
| `publicURL` | The `https` address people open, with no path, query, fragment or credentials. |
| `namespaces` | At least one, each a DNS label, none repeated. The console shows nothing outside them, whatever an account's RBAC allows. |
| `accounts[].id` | A DNS label, unique. It never changes, because RBAC bindings name it: a person who needs another ID gets another account. |
| `accounts[].login` | What the person types: ASCII letters, digits and punctuation, no spaces, at most 254 bytes. Unique ignoring case, and never starting with `system:`. It may change. |
| `accounts[].displayName` | Optional, at most 128 bytes, no control characters. |
| `trustedProxies` | Optional. The networks of the proxies in front of the console, in CIDR notation, at most 64, none repeated, none `/0`. Only from them is `X-Forwarded-For` read; see [Behind a proxy](serve.md#behind-a-proxy). |

A field the console does not know is refused, so a misspelled one is caught
instead of ignored.

Each account acts in Kubernetes as `ptah-console:<installationID>:<id>`, for
example `ptah-console:prod-eu-1:alex`. The server builds that name from this
file and nothing else; RoleBindings name it to grant the account its roles.

## The passwords file

An htpasswd file whose names are account IDs, one line per account:

```text
alex:$2a$12$...
sam:$2a$12$...
```

Only bcrypt is accepted (`$2a$`, `$2b$` or `$2y$`), at cost 10 or more. MD5
(`$apr1$`, `$1$`), SHA-1 (`{SHA}`), SHA-crypt (`$5$`, `$6$`), DES crypt and
plain text are refused. Every account needs exactly one line, and a line for
an ID the configuration does not have is refused. Blank lines and lines that
start with `#` are skipped. A refusal names the line and the account, never
the hash.

Make a hash with the console itself:

```sh
ptah-console accounts hash
```

At a terminal it asks twice and does not echo. Otherwise it reads one line
from standard input, so `pass show console/alex | ptah-console accounts hash`
works. It never takes the password as an argument, which would leave it in
shell history and the process list. A password longer than the 72 bytes
bcrypt reads is refused rather than cut short, and so is an empty one.
`--cost` sets the bcrypt cost; the default is 12.

## Checking both files

```sh
ptah-console config validate --config configuration.yaml --passwords passwords
```

reads both files the way the server does. For a valid pair it prints the
Kubernetes username of each account; otherwise it lists every problem and
exits with status 1.
