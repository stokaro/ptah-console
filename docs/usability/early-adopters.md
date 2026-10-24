# Usability check with early adopters

stokaro/ptah-operator#587 accepts the console only if a person can say why a
change is blocked and what the next supported step is without a kubectl
walkthrough. The acceptance rows prove that the console shows the right
facts. Whether a person who has not built it reads them correctly is a
separate question, and these sessions answer it (#41).

## Who and how

Two or three people who run PostgreSQL or MySQL schemas in Kubernetes and have
not used the console. One person facilitates and one takes notes. A session
takes about an hour: five tasks of up to ten minutes each, and a short
conversation at the end.

The facilitator reads each task aloud and does not explain the console, point
at the screen or name a page. The participant thinks aloud. A participant may
ask what a word means; the facilitator answers only with the operator's
documentation. The participant may not use kubectl. A terminal stays closed
for the whole session.

## The lab

On a Docker context the lab may use (see `make e2e-up`):

```sh
make e2e-up
./hack/usability/stage.sh
```

`stage.sh` puts the lab into the state each task starts from. It changes what
the acceptance rows start from, so run `make e2e-up` again before running
them.

Each task names the persona the participant signs in as. The facilitator
starts that persona's console and hands over the browser, already signed in:

```sh
make build
ns=$(sed -n 's/^NAMESPACE=//p' .e2e/out/facts)
./bin/ptah-console local --kubeconfig .e2e/out/<persona>.kubeconfig \
  --namespace "$ns" --namespace ptah-console-e2e-other
```

It prints the sign-in link; open it in the browser the participant will use.

Stop the console between tasks, so a participant never reaches the next
task's resource early.

## The tasks

Each task is read aloud as written. "Solved" lists what the participant has
to say in their own words. The notes record how long it took, where they
looked first and where they went wrong.

### 1. A release that does not arrive

Persona: `viewer`.

> The storefront team shipped version 3 of their schema an hour ago, and the
> database still has the old columns. Why, and what has to happen next?

Solved: the operator planned v3 and waits for an approval. The plan drops a
column and grants SELECT to every role. Someone with the approver role, other
than the person who wrote the change, has to review and approve that exact
plan. The participant cannot do it and can share the link.

### 2. A failed migration

Persona: `diag`.

> The journal service's latest deployment did not finish its database
> migrations. What went wrong, and what would you do about it?

Solved: migration 2 failed at its only statement with SQLSTATE 42P01 (a
table it reads does not exist) and committed nothing, and migration 1 is
recorded applied. The fix is a corrected artifact. The operator offers no
retry, and the participant does not look for one.

### 3. A run that stopped part-way

Persona: `diag`.

> The deliveries migrations ran on MySQL and something went wrong halfway.
> Is it safe to let the operator carry on, and who decides?

Solved: the run ended without proof of what it applied. On MySQL each DDL
statement commits on its own, so part of the migration may be in the
database. A person has to read the database and repair it. Someone allowed
to acknowledge runs then acknowledges the run, and only after that does the
operator read the history again and plan. The diagnostic role may not
acknowledge.

### 4. An outcome nobody can prove

Persona: `diag`.

> The reports migration was running when its Job disappeared. What does the
> operator know about what happened, and what is the next step?

Solved: the Apply Job was deleted while it ran, so the outcome is unknown,
not failed. Nothing proves what ran. The next step is the same as in task 3:
read the database, then have the run acknowledged. A retry is not offered.

### 5. What you are not shown

Persona: `viewer`.

> You were told there is a payroll schema, and that storefront's plan drops a
> column. Can you see either, and if not, why not?

Solved: the console was started for two namespaces, and this identity may not
read the second one, so payroll is not shown and the page says that namespace
is not readable rather than empty. The participant can see that storefront's
plan is destructive, but not its SQL: this identity may read the plan but not
the chunks the SQL is stored in.

## After the tasks

Ask, and note the answer word for word:

- Which task was hardest, and what would have helped?
- Was there a moment when you were sure of something and the page said
  otherwise?
- What did you expect to find that was not there?

## Findings

Each finding is an issue on stokaro/ptah-console with the task, the persona,
what the participant expected, what the page showed, and the notes' words for
it. A finding is filed whether or not it looks like a bug: a fact the page
showed that the participant did not read is as much a finding as a missing
one. A participant's name does not go in an issue.

The check passes when every participant solved every task without help. A
task one participant could not solve is a finding, and once it is fixed, that
task runs again with someone new.
