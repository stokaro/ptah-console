# Contributing to Ptah Console

[AGENTS.md](AGENTS.md) is the authority on how work is done here, for people
and coding agents alike. This page covers filing an issue and proposing a
change; it does not restate AGENTS.md.

## Where an issue belongs

The console shows and asks; the operator decides. A report about what a
condition means, why a plan waits, or how approvals are admitted belongs in
[stokaro/ptah-operator](https://github.com/stokaro/ptah-operator). A report
about how the console shows it, authenticates, or sends a request belongs
here.

A vulnerability does not go on the tracker. [Security policy](SECURITY.md) says
where it goes.

## Proposing a change

Open an issue before a change that alters what the console reads, how it
authenticates, or which actions it offers. Work on a branch off `origin/main`.

Run `make verify` before pushing.

### What a pull request says

Read `git log` before writing. An imperative subject under about 72
characters, a blank line, then a body that explains why. The diff shows what
changed; spend the body on the reason, the trade-off or the risk.

## Licensing of contributions

Ptah Console is MIT licensed. By contributing you agree that your contribution
is licensed under the same terms.

## Conduct

Participation is covered by the [Code of Conduct](CODE_OF_CONDUCT.md).
