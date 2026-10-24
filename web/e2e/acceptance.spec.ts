import { act, expect, fact, test } from "./lab"

// The acceptance rows of stokaro/ptah-operator#587, against the operator's own
// lab with the fixtures `make e2e-up` wrote. They run in this order, one
// person per console, and each is recorded. hack/e2e/verify.sh then checks
// against the cluster that every decision the rows made is the one they show.

const namespace = fact("NAMESPACE")
// How long the operator may take to apply a plan and verify it.
const applyTimeout = 15 * 60_000
// How long a plan may take to appear, when the operator plans again.
const planTimeout = 5 * 60_000

test("an outsider is told the namespace is not readable, not that it is empty", async ({
  page,
  lab,
}) => {
  await lab.signIn("outsider")
  await expect(page.getByText(fact("OUTSIDER")).first()).toBeVisible()
  await expect(page.getByText("Not shown, because this identity may not read them:")).toBeVisible()
  await expect(page.getByText(namespace, { exact: true }).first()).toBeVisible()
  await lab.audit("the approver queue of an identity with no access")
  await lab.caption(
    `${fact("OUTSIDER")} has no grant in ${namespace}. The console says so instead of showing an empty namespace.`
  )

  const response = await page.request.post(
    `${lab.origin()}/api/ptah.console.v1.ScopeService/ListNamespaces`,
    { data: {}, headers: { "Content-Type": "application/json", "Connect-Protocol-Version": "1" } }
  )
  expect(response.status()).toBe(200)
  const { namespaces } = await response.json()
  expect(namespaces).toEqual([
    expect.objectContaining({ name: namespace, access: "ACCESS_FORBIDDEN" }),
  ])
})

test("a viewer follows releases, sees a plan without its SQL, and nothing of another namespace", async ({
  page,
  lab,
}) => {
  const other = fact("OTHER_NAMESPACE")
  await lab.signIn("viewer", [namespace, other])
  await expect(page.getByText(fact("VIEWER")).first()).toBeVisible()
  await expect(page.getByText("Not shown, because this identity may not read them:")).toBeVisible()
  await expect(page.getByText(other, { exact: true }).first()).toBeVisible()
  await lab.caption(
    `This console was started for two namespaces. The viewer may read ${namespace} and nothing in ${other}, and the page says so.`,
    5000
  )
  // The schema in the other namespace is not in any answer this session
  // gets, and the namespace is reported as refused rather than as empty.
  const response = await page.request.post(
    `${lab.origin()}/api/ptah.console.v1.SchemaService/ListSchemas`,
    {
      data: { namespaces: [namespace, other] },
      headers: { "Content-Type": "application/json", "Connect-Protocol-Version": "1" },
    }
  )
  expect(response.status()).toBe(200)
  const listed = await response.json()
  expect(JSON.stringify(listed.schemas)).toContain("storefront")
  expect(JSON.stringify(listed.schemas)).not.toContain("payroll")
  expect(listed.namespaces).toContainEqual(
    expect.objectContaining({ namespace: other, access: "ACCESS_FORBIDDEN" })
  )
  // The home page picks the role an identity can act in; a viewer lands on
  // the developer's view, and opens the approver's queue to read it.
  await page.getByRole("link", { name: "Approver" }).click()
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const storefront = queue.getByRole("listitem").filter({ hasText: "for storefront" })
  await expect(storefront.getByRole("link", { name: /^ptah-plan-/ })).toBeVisible({
    timeout: planTimeout,
  })
  await expect(queue.getByRole("button", { name: "Approve…" })).toHaveCount(0)
  await lab.caption(
    "The viewer may read schemas, plans and decisions, and not the chunks a plan's SQL is stored in."
  )

  await storefront.getByRole("link", { name: "Review" }).click()
  await expect(page.getByText("You may not read this plan's SQL")).toBeVisible()
  await expect(
    page.getByText("This identity may read the plan but not the chunks its SQL is stored in", {
      exact: false,
    })
  ).toBeVisible()
  await expect(page.getByRole("list", { name: "SQL statements" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Approve this plan…" })).toHaveCount(0)
  await lab.audit("a plan whose SQL is not granted")
  await lab.caption(
    "The plan's manifest is shown; its SQL is not granted, and no approval is offered."
  )

  await page.getByRole("link", { name: "Ptah Console, home" }).click()
  await page.getByRole("link", { name: "Developer" }).click()
  const releases = page.getByRole("list", { name: "Releases" })
  const inventory = releases.getByRole("listitem").filter({ hasText: "inventory" })
  await expect(
    inventory.getByRole("list", { name: "Progress of inventory" }).getByText("Verify: done", {
      exact: true,
    })
  ).toBeVisible()
  await expect(
    releases
      .getByRole("listitem")
      .filter({ hasText: "storefront" })
      .getByRole("list", { name: "Progress of storefront" })
      .getByText("Approve: needs attention", { exact: true })
  ).toBeVisible()
  await lab.audit("the developer's releases")
  await lab.caption(
    "The developer view: inventory reached its database and was verified; storefront waits for an approver.",
    5000
  )

  // The full console opens on its overview, which lists only what needs
  // attention. Resources lists the same resources in one table, and still
  // names the namespace this identity may not read.
  await page.getByRole("link", { name: "Full console" }).click()
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible()
  await page
    .getByRole("navigation", { name: "Console" })
    .getByRole("link", { name: /^Resources/ })
    .click()
  await expect(page.getByRole("heading", { name: /^Resources/, level: 1 })).toBeVisible()
  const table = page.getByRole("table")
  const row = (name: string) =>
    table.getByRole("row").filter({ has: page.getByText(name, { exact: true }) })
  await expect(row("storefront").getByText("Waiting for approval")).toBeVisible()
  // The operator reads inventory again every interval, and while that read
  // runs the list shows it in progress; it is in sync again when it ends.
  await expect(row("inventory").getByText("In sync")).toBeVisible({ timeout: planTimeout })
  await expect(page.getByText("Not shown, because this identity may not read them:")).toBeVisible()
  await lab.audit("the full console's resources")
  await page
    .getByRole("group", { name: "State" })
    .getByRole("button", { name: /^In sync/ })
    .click()
  await expect(row("inventory")).toBeVisible({ timeout: planTimeout })
  await expect(row("storefront")).toHaveCount(0)
  await expect(page).toHaveURL(/state=insync/)
  await lab.caption(
    "The full console: every resource in one table, filtered here to what is in sync. The filter is in the address, so the list can be sent as a link.",
    5000
  )
})

test("a reviewer with get access reads the exact SQL from a shared link", async ({ page, lab }) => {
  await lab.signIn("reviewer")
  await expect(page.getByText(fact("REVIEWER")).first()).toBeVisible()
  await page.goto(`${lab.origin()}/approve/${namespace}/schema/storefront`)
  const sql = page.getByRole("list", { name: "SQL statements" })
  await expect(sql).toBeVisible({ timeout: planTimeout })
  await expect(sql).toContainText(/DROP COLUMN/i)
  await expect(sql).toContainText(/GRANT SELECT/i)
  await expect(
    page.getByText("Destructive: removes or rewrites data, as reported by the plan")
  ).toBeVisible()
  await expect(page.getByText("Changes privileges: Grant")).toBeVisible()
  await expect(page.getByRole("button", { name: "Approve this plan…" })).toHaveCount(0)
  await expect(
    page.getByText(`This identity may not approve plans in ${namespace}`, { exact: false })
  ).toBeVisible()
  await lab.audit("a plan read by a reviewer who may not approve")
  await lab.caption(
    "A SQL reviewer may only get schemas, plans and chunks. From a shared link they read the exact SQL: a DROP and a GRANT.",
    5500
  )
})

test("an author is not offered approval, and one sent anyway is forbidden", async ({
  page,
  lab,
}) => {
  const author = fact("AUTHOR")
  await lab.signIn("author")
  await expect(page.getByText(author).first()).toBeVisible()
  await page.getByRole("link", { name: "Approver" }).click()
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const storefront = queue.getByRole("listitem").filter({ hasText: "for storefront" })
  const planLink = storefront.getByRole("link", { name: /^ptah-plan-/ })
  await expect(planLink).toBeVisible({ timeout: planTimeout })
  const plan = (await planLink.textContent()) ?? ""
  await expect(queue.getByRole("button", { name: "Approve…" })).toHaveCount(0)
  await expect(page.getByText("no approval verb in these namespaces")).toBeVisible()
  await lab.caption(
    "The author may edit the schema and read its plan. RBAC gives them no approval verb."
  )

  const call = async (procedure: string, data: unknown) => {
    const response = await page.request.post(`${lab.origin()}/api/ptah.console.v1.${procedure}`, {
      data,
      headers: { "Content-Type": "application/json", "Connect-Protocol-Version": "1" },
    })
    expect(response.status(), `${procedure} answered`).toBe(200)
    return response.json()
  }
  const { plan: stored } = await call("SchemaService/GetSchemaPlan", {
    namespace,
    schema: "storefront",
    selection: "PLAN_SELECTION_CURRENT",
  })
  expect(stored.name).toBe(plan)
  // Sent the way a script would through the same session: the API server's
  // RBAC refuses it, and the console passes its words on.
  const { result } = await call("DecisionService/ApproveSchemaPlan", {
    namespace,
    schemaName: "storefront",
    schemaUid: stored.schemaUid,
    planName: stored.name,
    planUid: stored.uid,
    planFingerprint: stored.fingerprint,
    contractVersion: stored.contractVersion,
  })
  expect(result.outcome).toBe("DECISION_OUTCOME_FORBIDDEN")
  expect(result.message).toContain(`User "${author}" cannot create resource "ptahschemaapprovals"`)
})

test("a lead who wrote the change may not approve it", async ({ page, lab }) => {
  const lead = fact("LEAD")
  await lab.signIn("lead")
  await expect(page.getByText(lead).first()).toBeVisible()
  await lab.caption(
    `${lead} holds the author's and the approver's roles, and published storefront v3 themselves.`
  )
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const storefront = queue.getByRole("listitem").filter({ hasText: "for storefront" })
  await expect(storefront.getByRole("link", { name: /^ptah-plan-/ })).toBeVisible({
    timeout: planTimeout,
  })
  await storefront.getByRole("link", { name: "Review" }).click()
  await expect(page.getByRole("list", { name: "SQL statements" })).toBeVisible()
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Refused", { exact: true })).toBeVisible({ timeout: 30_000 })
  await expect(
    dialog.getByText(`schema requires a distinct approver, and ${lead} last changed its spec`, {
      exact: false,
    })
  ).toBeVisible()
  await lab.caption(
    "The installation requires a distinct approver. The operator's admission refused the lead's own approval, in its own words.",
    5500
  )
})

test("an approver approves a plan that drops a column and grants access, and follows it to the database", async ({
  page,
  lab,
}) => {
  const approver = fact("APPROVER")
  await lab.signIn("approver")
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const storefront = queue.getByRole("listitem").filter({ hasText: "for storefront" })
  const planLink = storefront.getByRole("link", { name: /^ptah-plan-/ })
  await expect(planLink).toBeVisible({ timeout: planTimeout })
  const plan = (await planLink.textContent()) ?? ""
  await expect(page.getByText(approver).first()).toBeVisible()
  await lab.audit("the approver queue")
  await lab.caption(
    `Signed in from the link ptah-console printed. The console acts as ${approver}, a client certificate the cluster issued.`
  )
  await lab.caption(
    "The operator planned storefront v3 and waits for an approver: three plans wait here."
  )

  await storefront.getByRole("link", { name: "Review" }).click()
  await expect(
    page.getByText("Destructive: removes or rewrites data, as reported by the plan")
  ).toBeVisible()
  await expect(page.getByText("Changes privileges: Grant")).toBeVisible()
  await lab.caption(
    "What this changes, as the plan itself reports it: it removes data, and it grants a privilege."
  )
  const sql = page.getByRole("list", { name: "SQL statements" })
  await expect(sql).toContainText(/DROP COLUMN/i)
  await expect(sql).toContainText(/GRANT SELECT/i)
  await lab.audit("a plan with a DROP and a GRANT")
  await lab.caption(
    "The exact SQL, read through the operator's integrity-checked plan reader, shown as plain text.",
    4500
  )
  await page.mouse.wheel(0, 500)
  await lab.caption(
    "The approval binds this plan, its UID and its fingerprint. Nothing else is sent."
  )

  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await expect(dialog.getByText(approver)).toBeVisible()
  await lab.audit("the approval dialog")
  await lab.caption("The dialog says where the decision lands and whom it is sent as.", 3000)
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await expect(
    dialog.getByText(`recorded by admission as ${approver}`, { exact: false })
  ).toBeVisible()
  await lab.caption("Admission stamped the approver. Approved is not yet applied.", 4000)

  await dialog.getByRole("link", { name: "Follow it" }).click()
  const progress = page.getByRole("list", { name: "Progress of storefront" })
  await expect(progress).toBeVisible()
  await lab.caption(
    "The page follows the schema live while the operator applies the plan in a Job."
  )
  await lab.skip(() =>
    expect(progress.getByText("Verify: done", { exact: true })).toBeVisible({
      timeout: applyTimeout,
    })
  )
  await lab.caption("Applied, and verified by a fresh read of the database.", 4500)
  expect(plan).toMatch(/^ptah-plan-/)
  await lab.audit("a release that reached its database")

  // The full console's page for the schema names the plan it applied.
  await page.goto(`${lab.origin()}/console/schemas/${namespace}/storefront`)
  await expect(page.getByRole("region", { name: "Applied" })).toContainText(plan)
  await expect(page.getByRole("region", { name: "Target" })).toContainText("Secret")
  await expect(page.getByLabel("Spec of storefront")).toContainText("urlFrom")
  await lab.audit("a schema in the full console")
  await lab.caption(
    "The full console's page for storefront: the plan it applied, every condition, the artifact, the target by Secret reference, and the spec, read-only.",
    5500
  )
})

test("an approver whose plan is replaced while they decide is told nothing was approved", async ({
  page,
  lab,
}) => {
  const before = await act("plan", "ptahschema", "storefront")
  await act("publish", "storefront", "v4")
  const v4 = await act("wait-plan", "ptahschema", "storefront", before)
  await lab.signIn("approver")
  await page.goto(`${lab.origin()}/approve/${namespace}/schema/storefront`)
  const sql = page.getByRole("list", { name: "SQL statements" })
  await expect(sql).toContainText(/placed_at/, { timeout: planTimeout })
  await lab.caption(`storefront v4 adds a column. The approver reads plan ${v4}.`)
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await lab.caption("While the dialog is open, another team publishes storefront v5.")

  await act("publish", "storefront", "v5")
  const v5 = await lab.skip(() => act("wait-plan", "ptahschema", "storefront", v4))
  await lab.caption("The operator has planned v5. The approver confirms what they reviewed: v4.")
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("It changed", { exact: true })).toBeVisible({ timeout: 30_000 })
  await expect(
    dialog.getByText("A newer plan replaced the one you reviewed. Nothing was approved.")
  ).toBeVisible()
  await lab.caption(
    "The console read the schema again before sending. A newer plan replaced the reviewed one, so nothing was approved.",
    5500
  )

  await dialog.getByRole("link", { name: "Review the new plan" }).click()
  await expect(page.getByText("The plan changed", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Approve this plan…" })).toBeDisabled()
  await expect(sql).toContainText(/note/)
  await lab.caption(
    `The page shows plan ${v5}, and keeps approval off until the approver says they are reading it.`,
    4500
  )
  await page.getByRole("button", { name: "I am reading the new plan" }).click()
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await expect(dialog.getByText(v5, { exact: false })).toBeVisible()
  await dialog.getByRole("link", { name: "Follow it" }).click()
  const progress = page.getByRole("list", { name: "Progress of storefront" })
  await lab.skip(() =>
    expect(progress.getByText("Verify: done", { exact: true })).toBeVisible({
      timeout: applyTimeout,
    })
  )
  await lab.caption("The plan the approver read is the plan that ran.", 4000)
})

test("an approver approves migrations 14 to 17 without their SQL", async ({ page, lab }) => {
  await lab.signIn("approver")
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const billing = queue.getByRole("listitem").filter({ hasText: "for billing" })
  await expect(billing.getByRole("link", { name: /^ptah-/ })).toBeVisible({ timeout: planTimeout })
  await billing.getByRole("link", { name: "Review" }).click()
  const steps = page.getByRole("table", { name: "Steps in order" })
  await expect(steps).toBeVisible()
  for (const version of ["14", "15", "16", "17"]) {
    // The version as the plan spells it: Ptah's directories pad it.
    await expect(steps.getByRole("cell", { name: new RegExp(`^0*${version}$`) })).toBeVisible()
  }
  await expect(page.getByText("SQL is not available through this operator API")).toBeVisible()
  await lab.audit("a migration plan")
  await lab.caption(
    "A migration plan lists its steps in order with their checksums. The operator API carries no SQL for them, and the page says so.",
    5500
  )
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await dialog.getByRole("link", { name: "Follow it" }).click()
  const progress = page.getByRole("list", { name: "Progress of billing" })
  await lab.skip(() =>
    expect(progress.getByText("Verify history: done", { exact: true })).toBeVisible({
      timeout: applyTimeout,
    })
  )
  await lab.caption("Applied, and the database's own history read again.", 4000)
})

test("on call, a failed run shows its evidence and the next supported step", async ({
  page,
  lab,
}) => {
  await act("fail", "journal")
  await lab.signIn("diag")
  await expect(page.getByText(fact("DIAG")).first()).toBeVisible()
  await page.getByRole("link", { name: "On call" }).click()
  const incidents = page.getByRole("list", { name: "Things that need a person" })
  const journal = incidents.getByRole("link", { name: /^(Failed|Blocked): journal$/ })
  await lab.caption(
    "journal's second migration reads a table that is not there. The operator runs it."
  )
  await lab.skip(() => expect(journal).toBeVisible({ timeout: applyTimeout }))
  await expect(
    page.getByText("Retry, rollback and force apply are not offered", { exact: false })
  ).toBeVisible()
  await lab.audit("the on-call list")
  await lab.caption(
    "The on-call view lists what needs a person. Retry, rollback and force apply are not part of the operator's contract."
  )

  await journal.click()
  const evidence = page.getByRole("list", { name: "Evidence" })
  await expect(evidence.getByText(/^Job /).first()).toBeVisible()
  // The excerpt is the failed run's: the Apply Job the status names as the
  // last run, and the container that ran the migrations, not the history
  // read the operator started since or an init container. The runner logs
  // only how Ptah exited. The status names the migration that failed, the
  // statement in it and the database's error code: the old ledger table was
  // never created, so PostgreSQL answers 42P01.
  await expect(
    page.getByRole("region", {
      name: /^Log excerpt · ptah-m-apply-journal-\S+ \/ ptah · plain text$/,
    })
  ).toBeVisible()
  const log = page.getByLabel("Log excerpt", { exact: true })
  await expect(log).toContainText("ptah exited with code")
  await expect(
    page
      .getByText(
        "Migration 2 (Backfill Entries) failed at statement 1 of 1 with SQLSTATE 42P01 and committed nothing; 1 migrations before it are recorded applied",
        { exact: false }
      )
      .first()
  ).toBeVisible()
  await lab.audit("an incident with its evidence")
  await lab.caption(
    "The evidence Kubernetes still keeps: the Job the status names, its Pod and Events, and the end of its log as plain text.",
    6000
  )
  await log.scrollIntoViewIfNeeded()
  await lab.caption(
    "The status names the migration that failed, the statement and the database's error code. The log says how Ptah exited.",
    5500
  )

  // The full console reaches the same run from its palette.
  await page.getByRole("link", { name: "Full console" }).click()
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible()
  await page.keyboard.press("ControlOrMeta+k")
  await page.getByRole("combobox", { name: "Search pages and resources" }).fill("journal")
  await page.keyboard.press("Enter")
  await expect(page.getByRole("heading", { name: /PtahMigration \/ journal/ })).toBeVisible()
  await expect(page.getByRole("region", { name: "Last run" })).toContainText(
    "Migration 2 (Backfill Entries) failed"
  )
  await lab.audit("a migration in the full console")
  await lab.caption(
    "The full console's page for journal: how far its history is, how the last run ended, and what a person may do. Retry is not among it.",
    5500
  )
  await page.getByRole("link", { name: "Execution and history" }).click()
  await expect(page.getByRole("region", { name: "Original error" })).toBeVisible()
  await expect(page.getByRole("table", { name: "Execution and history of journal" })).toContainText(
    /Job ptah-m-apply-journal-/
  )
  await lab.audit("a migration's execution and history")
  await lab.caption(
    "Its execution and history: the controller, the Job and the database on their own lanes, with the original error pinned.",
    5500
  )
  await page.getByRole("link", { name: "Pre-flight" }).click()
  const matrix = page.getByRole("table", { name: "What this identity may do, by namespace" })
  const answers = matrix.getByRole("row", { name: new RegExp(`^${namespace}`) })
  await expect(answers.getByRole("img", { name: "Read logs: Allowed" })).toBeVisible()
  await expect(answers.getByRole("img", { name: "Approve schema plans: Denied" })).toBeVisible()
  await lab.audit("pre-flight")
  await lab.caption(
    "Pre-flight: what this identity may do in each namespace, as the API server answers it. The diagnostic reader may read logs and may not approve.",
    5500
  )
})

test("an outcome nobody can prove is settled by an acknowledgment, not a retry", async ({
  page,
  lab,
}) => {
  const approver = fact("APPROVER")
  await lab.signIn("approver")
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const deliveries = queue.getByRole("listitem").filter({ hasText: "for deliveries" })
  await expect(deliveries.getByRole("link", { name: /^ptah-/ })).toBeVisible({
    timeout: planTimeout,
  })
  await deliveries.getByRole("link", { name: "Review" }).click()
  await expect(page.getByRole("table", { name: "Steps in order" })).toContainText("Add Status")
  await lab.caption(
    "deliveries runs on MySQL, where each DDL statement commits on its own. Its next migration fails on its last statement."
  )
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await dialog.getByRole("button", { name: "Close" }).click()
  await lab.skip(() => act("wait-run", "deliveries", "Partial"))

  await lab.signIn("diag")
  await page.getByRole("link", { name: "On call" }).click()
  const unresolved = page
    .getByRole("list", { name: "Things that need a person" })
    .getByRole("link", { name: "Unresolved run: deliveries" })
  await expect(unresolved).toBeVisible({ timeout: planTimeout })
  await unresolved.click()
  await expect(page.getByText(/ended without proof of what it applied/)).toBeVisible()
  await expect(
    page.getByText(`This identity may not acknowledge runs in ${namespace}.`)
  ).toBeVisible()
  await lab.caption(
    "Two of three statements committed. The operator records the run as unresolved and runs nothing until a person settles it.",
    5500
  )
  await lab.caption(
    "The on-call engineer reads the evidence; settling the run is the approver's decision."
  )
  await act("repair", "deliveries")
  await lab.caption("A DBA reads the revision table and undoes the two statements that committed.")

  await lab.signIn("approver")
  await page.getByRole("link", { name: "On call" }).click()
  await page
    .getByRole("list", { name: "Things that need a person" })
    .getByRole("link", { name: "Unresolved run: deliveries" })
    .click()
  await page.getByRole("button", { name: "Acknowledge this run…" }).click()
  await expect(
    dialog.getByText("It is not evidence that the SQL effect was verified.", { exact: false })
  ).toBeVisible()
  await lab.caption(
    "The acknowledgment settles exactly this run. It does not claim the SQL was verified."
  )
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Acknowledgment admitted")).toBeVisible({ timeout: 30_000 })
  await expect(dialog.getByText(`Recorded by admission as ${approver}`)).toBeVisible()
  await dialog.getByRole("button", { name: "Close" }).click()
  await page.getByRole("link", { name: "Ptah Console, home" }).click()
  await page.getByRole("link", { name: "On call" }).click()
  await lab.skip(() =>
    expect(page.getByRole("link", { name: "Unresolved run: deliveries" })).toHaveCount(0, {
      timeout: planTimeout,
    })
  )
  // Settled is settled: the run is not reported as a failure either.
  await expect(page.getByRole("link", { name: /: deliveries$/ })).toHaveCount(0)
  await lab.caption(
    "The operator settled the run in the approver's name and read the history again.",
    4500
  )
})

test("a run whose Job was taken away is unknown until acknowledged, and an API outage holds the decision", async ({
  page,
  lab,
}) => {
  const approver = fact("APPROVER")
  await act("run", "reports")
  await lab.signIn("approver")
  await page.getByRole("link", { name: "On call" }).click()
  await lab.caption(
    "reports' second migration holds its run open for a minute. While it runs, its Job is deleted."
  )
  await lab.skip(() => act("interrupt", "reports"))
  const unresolved = page
    .getByRole("list", { name: "Things that need a person" })
    .getByRole("link", { name: "Unresolved run: reports" })
  await expect(unresolved).toBeVisible({ timeout: planTimeout })
  await unresolved.click()
  await expect(page.getByText(/ended without proof of what it applied/)).toBeVisible()
  const acknowledge = page.getByRole("button", { name: "Acknowledge this run…" })
  await expect(acknowledge).toBeVisible()
  await lab.caption(
    "Nothing says what the run did, so the operator records it as unresolved and starts nothing on this database until a person settles it.",
    5500
  )

  await lab.caption("Now the API server stops answering.", 3500, "bottom")
  await act("outage", "start")
  try {
    const banner = page.getByText(/^Live updates stopped at /)
    await lab.skip(() => expect(banner).toBeVisible({ timeout: 3 * 60_000 }))
    await expect(acknowledge).toHaveCount(0)
    await expect(
      page.getByText("The console lost touch with the cluster.", { exact: false })
    ).toBeVisible()
    await lab.audit("a page that lost touch with the cluster")
    await lab.caption(
      "The page says since when it may be behind, and offers no decision until the console is in touch again.",
      6000,
      "bottom"
    )
  } finally {
    await act("outage", "stop")
  }
  await lab.skip(() =>
    expect(page.getByText(/^Live updates stopped at /)).toHaveCount(0, { timeout: 3 * 60_000 })
  )
  await expect(acknowledge).toBeVisible()
  await lab.caption(
    "The API server answers again, and the page catches up on its own.",
    3500,
    "bottom"
  )

  await act("repair", "reports")
  await lab.caption(
    "The interrupted migration only slept. Once its session is gone, a DBA removes its unfinished revision."
  )
  await acknowledge.click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Acknowledgment admitted")).toBeVisible({ timeout: 30_000 })
  await expect(dialog.getByText(`Recorded by admission as ${approver}`)).toBeVisible()
  await dialog.getByRole("button", { name: "Close" }).click()
  await page.getByRole("link", { name: "Ptah Console, home" }).click()
  await page.getByRole("link", { name: "Developer" }).click()
  const progress = page.getByRole("list", { name: "Progress of reports" })
  await lab.skip(() =>
    expect(progress.getByText("Verify history: done", { exact: true })).toBeVisible({
      timeout: applyTimeout,
    })
  )
  await lab.caption(
    "The operator read the history again, ran the migration once more, and verified it.",
    4500
  )
})

test("an auditor reads who decided what, as admission recorded it", async ({ page, lab }) => {
  const approver = fact("APPROVER")
  await lab.signIn("approver")
  await page.getByRole("link", { name: "Auditor" }).click()
  const decisions = page.getByRole("list", { name: "Decisions" })
  await expect(decisions.getByRole("link", { name: /^Approved .* for storefront/ })).toHaveCount(2)
  await expect(decisions.getByRole("link", { name: /^Approved .* for billing/ })).toHaveCount(1)
  await expect(decisions.getByRole("link", { name: /^Approved .* for deliveries/ })).toHaveCount(1)
  await expect(
    decisions.getByRole("link", { name: /^Acknowledged run .* of deliveries/ })
  ).toHaveCount(1)
  await expect(
    decisions.getByRole("link", { name: /^Acknowledged run .* of reports/ })
  ).toHaveCount(1)
  await expect(decisions.getByText(fact("LEAD"))).toHaveCount(0)
  await lab.audit("the auditor's decisions")
  await lab.caption(
    "Every decision on record, with who made it as admission stamped it. The refused ones were never stored.",
    5500
  )
  const applied = await act("applied", "storefront")
  await decisions
    .getByRole("link", { name: new RegExp(`^Approved ${applied} for storefront`) })
    .click()
  await expect(
    page.getByText(`by ${approver}, recorded by admission`, { exact: false })
  ).toBeVisible()
  await expect(page.getByLabel("Approved SQL", { exact: true })).toContainText(/note/)
  await lab.audit("one decision with its approved SQL")
  await lab.caption(
    "The exact SQL that was approved, read from the plan the operator still keeps.",
    5000
  )
})
