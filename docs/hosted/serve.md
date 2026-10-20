# Serving a hosted console

```sh
ptah-console serve --config configuration.yaml --passwords passwords \
  --tls-cert-file tls.crt --tls-key-file tls.key
```

`serve` runs the console beside the operator; [the chart](../../charts/ptah-console/README.md)
installs it that way. People sign in with the
accounts in the configuration (see [Hosted accounts](accounts.md)), and each
account acts in Kubernetes as `ptah-console:<installationID>:<id>`. The console
reaches that identity through its own ServiceAccount, which may impersonate
exactly those usernames and nothing else. A user call never runs as the
ServiceAccount itself.

## HTTPS

A hosted console serves HTTPS, or it does not start. Give it one of these:

- `--tls-cert-file` and `--tls-key-file`: the console terminates TLS itself.
  It reads the files again when either changes, so a certificate a manager
  renews in its Secret is served without a restart.
- `--tls-terminated-upstream`: an ingress or load balancer in front of the
  console terminates TLS, and the console serves plain HTTP to it. Declaring
  this is a statement about your network; the console cannot check it.

The session cookie is `__Host-ptah_console`, `Secure`, `HttpOnly` and
`SameSite=Strict`, in both cases.

The console answers to the host in `publicURL` and to nothing else, so a
proxy has to pass the original `Host` header on. A request that carries an
`Impersonate-*` or `X-Remote-*` header is refused. `GET /healthz` is the one
path that answers any host, for the kubelet's probes, and it says `ok` and
nothing more.

## Signing in

An unknown login and a wrong password get the same answer, and take as long:
an unknown login is compared against a hash made at start at the accounts'
cost. A password longer than the 72 bytes bcrypt reads is refused, never
cut short.

Attempts are limited before any hash is compared: five per login at once and
one more every 12 seconds, thirty per source address and one more every 2
seconds. An IPv6 address counts under its /64, so one network cannot spread
its attempts over its own addresses.

### Behind a proxy

Behind an ingress or a load balancer every connection comes from the proxy,
so one address limit would hold everyone. Name the proxies' networks in
`trustedProxies`, and the console reads `X-Forwarded-For` to find each
client:

- The header is read only when the connection comes from a trusted network.
  From anywhere else it is ignored, whatever it says.
- It is read from the right. Each proxy appends the address it received the
  request from, so the console skips trusted proxies and stops at the first
  address that is not one. Anything a client wrote into the header itself is
  to the left of that and is never read.
- An entry it cannot read in the part it has to read, or more than 16 trusted
  hops, means the header is not believed, and the connection's address
  counts.
- `/0` is refused: a network that holds every address would let any client
  name any other.

Name only the proxies themselves, such as the ingress controller's Pod
network, and make sure clients cannot reach the console's Service around them.
A NetworkPolicy that admits only the proxies does that; the chart's example
policy takes them in `networkPolicy.ingressFrom`. The resolved address is
written into the audit line of every sign-in.

Signing in replaces the session the browser had. A session ends after 30
minutes without an action or 8 hours after sign-in, on sign-out, and when the
console restarts: sessions live in the one replica's memory, so a rollout
signs everyone out, which is also how a changed password takes effect.

## Flags

| Flag | Meaning |
| --- | --- |
| `--config`, `--passwords` | The two files. Both are required. |
| `--listen` | The address to serve on; `:8443` by default. |
| `--tls-cert-file`, `--tls-key-file` | Serve HTTPS with this certificate. |
| `--tls-terminated-upstream` | Serve plain HTTP to a proxy that terminates TLS. |
| `--read-only` | Refuse every change, whatever an account may do. |
| `--kubeconfig` | The console's own identity when it runs outside a cluster. In a Pod it uses its ServiceAccount. |
