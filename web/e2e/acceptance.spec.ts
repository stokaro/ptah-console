import { act, expect, fact, test } from "./console"

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
  console,
}) => {
  await console.signIn("outsider")
  await expect(page.getByText(fact("OUTSIDER")).first()).toBeVisible()
  await expect(page.getByText("Not shown, because this identity may not read them:")).toBeVisible()
  await expect(page.getByText(namespace, { exact: true }).first()).toBeVisible()
  await console.caption(
    `${fact("OUTSIDER")} has no grant in ${namespace}. The console says so instead of showing an empty namespace.`
  )

  const response = await page.request.post(
    `${console.origin()}/api/ptah.console.v1.ScopeService/ListNamespaces`,
    { data: {}, headers: { "Content-Type": "application/json", "Connect-Protocol-Version": "1" } }
  )
  expect(response.status()).toBe(200)
  const { namespaces } = await response.json()
  expect(namespaces).toEqual([
    expect.objectContaining({ name: namespace, access: "ACCESS_FORBIDDEN" }),
  ])
})

test("a viewer follows releases and sees a plan without its SQL", async ({ page, console }) => {
  await console.signIn("viewer")
  await expect(page.getByText(fact("VIEWER")).first()).toBeVisible()
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const storefront = queue.getByRole("listitem").filter({ hasText: "for storefront" })
  await expect(storefront.getByRole("link", { name: /^ptah-plan-/ })).toBeVisible({
    timeout: planTimeout,
  })
  await expect(queue.getByRole("button", { name: "Approve…" })).toHaveCount(0)
  await console.caption(
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
  await console.caption(
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
  await console.caption(
    "The developer view: inventory reached its database and was verified; storefront waits for an approver.",
    5000
  )
})

test("a reviewer with get access reads the exact SQL from a shared link", async ({
  page,
  console,
}) => {
  await console.signIn("reviewer")
  await expect(page.getByText(fact("REVIEWER")).first()).toBeVisible()
  await page.goto(`${console.origin()}/approve/${namespace}/schema/storefront`)
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
  await console.caption(
    "A SQL reviewer may only get schemas, plans and chunks. From a shared link they read the exact SQL: a DROP and a GRANT.",
    5500
  )
})

test("an author is not offered approval, and one sent anyway is forbidden", async ({
  page,
  console,
}) => {
  const author = fact("AUTHOR")
  await console.signIn("author")
  await expect(page.getByText(author).first()).toBeVisible()
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const storefront = queue.getByRole("listitem").filter({ hasText: "for storefront" })
  const planLink = storefront.getByRole("link", { name: /^ptah-plan-/ })
  await expect(planLink).toBeVisible({ timeout: planTimeout })
  const plan = (await planLink.textContent()) ?? ""
  await expect(queue.getByRole("button", { name: "Approve…" })).toHaveCount(0)
  await expect(page.getByText("no approval verb in these namespaces")).toBeVisible()
  await console.caption(
    "The author may edit the schema and read its plan. RBAC gives them no approval verb."
  )

  const call = async (procedure: string, data: unknown) => {
    const response = await page.request.post(
      `${console.origin()}/api/ptah.console.v1.${procedure}`,
      {
        data,
        headers: { "Content-Type": "application/json", "Connect-Protocol-Version": "1" },
      }
    )
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

test("a lead who wrote the change may not approve it", async ({ page, console }) => {
  const lead = fact("LEAD")
  await console.signIn("lead")
  await expect(page.getByText(lead).first()).toBeVisible()
  await console.caption(
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
  await console.caption(
    "The installation requires a distinct approver. The operator's admission refused the lead's own approval, in its own words.",
    5500
  )
})

test("an approver approves a plan that drops a column and grants access, and follows it to the database", async ({
  page,
  console,
}) => {
  const approver = fact("APPROVER")
  await console.signIn("approver")
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const storefront = queue.getByRole("listitem").filter({ hasText: "for storefront" })
  const planLink = storefront.getByRole("link", { name: /^ptah-plan-/ })
  await expect(planLink).toBeVisible({ timeout: planTimeout })
  const plan = (await planLink.textContent()) ?? ""
  await expect(page.getByText(approver).first()).toBeVisible()
  await console.caption(
    `Signed in from the link ptah-console printed. The console acts as ${approver}, a client certificate the cluster issued.`
  )
  await console.caption(
    "The operator planned storefront v3 and waits for an approver: three plans wait here."
  )

  await storefront.getByRole("link", { name: "Review" }).click()
  await expect(
    page.getByText("Destructive: removes or rewrites data, as reported by the plan")
  ).toBeVisible()
  await expect(page.getByText("Changes privileges: Grant")).toBeVisible()
  await console.caption(
    "What this changes, as the plan itself reports it: it removes data, and it grants a privilege."
  )
  const sql = page.getByRole("list", { name: "SQL statements" })
  await expect(sql).toContainText(/DROP COLUMN/i)
  await expect(sql).toContainText(/GRANT SELECT/i)
  await console.caption(
    "The exact SQL, read through the operator's integrity-checked plan reader, shown as plain text.",
    4500
  )
  await page.mouse.wheel(0, 500)
  await console.caption(
    "The approval binds this plan, its UID and its fingerprint. Nothing else is sent."
  )

  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await expect(dialog.getByText(approver)).toBeVisible()
  await console.caption("The dialog says where the decision lands and whom it is sent as.", 3000)
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await expect(
    dialog.getByText(`recorded by admission as ${approver}`, { exact: false })
  ).toBeVisible()
  await console.caption("Admission stamped the approver. Approved is not yet applied.", 4000)

  await dialog.getByRole("link", { name: "Follow it" }).click()
  const progress = page.getByRole("list", { name: "Progress of storefront" })
  await expect(progress).toBeVisible()
  await console.caption(
    "The page follows the schema live while the operator applies the plan in a Job."
  )
  await console.skip(() =>
    expect(progress.getByText("Verify: done", { exact: true })).toBeVisible({
      timeout: applyTimeout,
    })
  )
  await console.caption("Applied, and verified by a fresh read of the database.", 4500)
  expect(plan).toMatch(/^ptah-plan-/)
})

test("an approver whose plan is replaced while they decide is told nothing was approved", async ({
  page,
  console,
}) => {
  const before = await act("plan", "ptahschema", "storefront")
  await act("publish", "storefront", "v4")
  const v4 = await act("wait-plan", "ptahschema", "storefront", before)
  await console.signIn("approver")
  await page.goto(`${console.origin()}/approve/${namespace}/schema/storefront`)
  const sql = page.getByRole("list", { name: "SQL statements" })
  await expect(sql).toContainText(/placed_at/, { timeout: planTimeout })
  await console.caption(`storefront v4 adds a column. The approver reads plan ${v4}.`)
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await console.caption("While the dialog is open, another team publishes storefront v5.")

  await act("publish", "storefront", "v5")
  const v5 = await console.skip(() => act("wait-plan", "ptahschema", "storefront", v4))
  await console.caption(
    "The operator has planned v5. The approver confirms what they reviewed: v4."
  )
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("It changed", { exact: true })).toBeVisible({ timeout: 30_000 })
  await expect(
    dialog.getByText("A newer plan replaced the one you reviewed. Nothing was approved.")
  ).toBeVisible()
  await console.caption(
    "The console read the schema again before sending. A newer plan replaced the reviewed one, so nothing was approved.",
    5500
  )

  await dialog.getByRole("link", { name: "Review the new plan" }).click()
  await expect(page.getByText("The plan changed", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Approve this plan…" })).toBeDisabled()
  await expect(sql).toContainText(/note/)
  await console.caption(
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
  await console.skip(() =>
    expect(progress.getByText("Verify: done", { exact: true })).toBeVisible({
      timeout: applyTimeout,
    })
  )
  await console.caption("The plan the approver read is the plan that ran.", 4000)
})

test("an approver approves migrations 14 to 17 without their SQL", async ({ page, console }) => {
  await console.signIn("approver")
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
  await console.caption(
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
  await console.skip(() =>
    expect(progress.getByText("Verify history: done", { exact: true })).toBeVisible({
      timeout: applyTimeout,
    })
  )
  await console.caption("Applied, and the database's own history read again.", 4000)
})

test("on call, a failed run shows its evidence and the next supported step", async ({
  page,
  console,
}) => {
  await act("fail", "journal")
  await console.signIn("diag")
  await expect(page.getByText(fact("DIAG")).first()).toBeVisible()
  await page.getByRole("link", { name: "On call" }).click()
  const incidents = page.getByRole("list", { name: "Things that need a person" })
  const journal = incidents.getByRole("link", { name: /^(Failed|Blocked): journal$/ })
  await console.caption(
    "journal's second migration reads a table that is not there. The operator runs it."
  )
  await console.skip(() => expect(journal).toBeVisible({ timeout: applyTimeout }))
  await expect(
    page.getByText("Retry, rollback and force apply are not offered", { exact: false })
  ).toBeVisible()
  await console.caption(
    "The on-call view lists what needs a person. Retry, rollback and force apply are not part of the operator's contract."
  )

  await journal.click()
  const evidence = page.getByRole("list", { name: "Evidence" })
  await expect(evidence.getByText(/^Job /).first()).toBeVisible()
  await expect(page.getByText(/ledger_entries/).first()).toBeVisible()
  await expect(page.getByLabel("Log excerpt")).toBeVisible()
  await console.caption(
    "The evidence Kubernetes still keeps: the Job the status names, its Pod and Events, and the end of its log as plain text.",
    6000
  )
  await page.getByLabel("Log excerpt").scrollIntoViewIfNeeded()
  await console.caption("The run's own words say what failed.", 4500)
})

test("an outcome nobody can prove is settled by an acknowledgment, not a retry", async ({
  page,
  console,
}) => {
  const approver = fact("APPROVER")
  await console.signIn("approver")
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const deliveries = queue.getByRole("listitem").filter({ hasText: "for deliveries" })
  await expect(deliveries.getByRole("link", { name: /^ptah-/ })).toBeVisible({
    timeout: planTimeout,
  })
  await deliveries.getByRole("link", { name: "Review" }).click()
  await expect(page.getByRole("table", { name: "Steps in order" })).toContainText("Add Status")
  await console.caption(
    "deliveries runs on MySQL, where each DDL statement commits on its own. Its next migration fails on its last statement."
  )
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await dialog.getByRole("button", { name: "Close" }).click()
  await console.skip(() => act("wait-run", "deliveries", "Partial"))

  await console.signIn("diag")
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
  await console.caption(
    "Two of three statements committed. The operator records the run as unresolved and runs nothing until a person settles it.",
    5500
  )
  await console.caption(
    "The on-call engineer reads the evidence; settling the run is the approver's decision."
  )
  await act("repair", "deliveries")
  await console.caption(
    "A DBA reads the revision table and undoes the two statements that committed."
  )

  await console.signIn("approver")
  await page.getByRole("link", { name: "On call" }).click()
  await page
    .getByRole("list", { name: "Things that need a person" })
    .getByRole("link", { name: "Unresolved run: deliveries" })
    .click()
  await page.getByRole("button", { name: "Acknowledge this run…" }).click()
  await expect(
    dialog.getByText("It is not evidence that the SQL effect was verified.", { exact: false })
  ).toBeVisible()
  await console.caption(
    "The acknowledgment settles exactly this run. It does not claim the SQL was verified."
  )
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Acknowledgment admitted")).toBeVisible({ timeout: 30_000 })
  await expect(dialog.getByText(`Recorded by admission as ${approver}`)).toBeVisible()
  await dialog.getByRole("button", { name: "Close" }).click()
  await page.getByRole("link", { name: "Ptah Console, home" }).click()
  await page.getByRole("link", { name: "On call" }).click()
  await console.skip(() =>
    expect(page.getByRole("link", { name: "Unresolved run: deliveries" })).toHaveCount(0, {
      timeout: planTimeout,
    })
  )
  await console.caption(
    "The operator settled the run in the approver's name and read the history again.",
    4500
  )
})

test("when the API server stops answering, the page says so and holds decisions", async ({
  page,
  console,
}) => {
  await console.signIn("approver")
  await page.goto(`${console.origin()}/approve/${namespace}/migration/deliveries`)
  const approve = page.getByRole("button", { name: "Approve this plan…" })
  await expect(approve).toBeVisible({ timeout: planTimeout })
  await console.caption(
    "deliveries waits for an approval again. Now the API server stops answering."
  )
  await act("outage", "start")
  try {
    const banner = page.getByText(/^Live updates stopped at /)
    await console.skip(() => expect(banner).toBeVisible({ timeout: 3 * 60_000 }))
    await expect(approve).toHaveCount(0)
    await expect(
      page.getByText("The console lost touch with the cluster.", { exact: false })
    ).toBeVisible()
    await console.caption(
      "The page says since when it may be behind, and offers no decision until the console is in touch again.",
      6000
    )
  } finally {
    await act("outage", "stop")
  }
  await console.skip(() =>
    expect(page.getByText(/^Live updates stopped at /)).toHaveCount(0, { timeout: 3 * 60_000 })
  )
  await expect(approve).toBeVisible()
  await console.caption("The API server answers again. The page catches up on its own.", 4500)
})

test("an auditor reads who decided what, as admission recorded it", async ({ page, console }) => {
  const approver = fact("APPROVER")
  await console.signIn("approver")
  await page.getByRole("link", { name: "Auditor" }).click()
  const decisions = page.getByRole("list", { name: "Decisions" })
  await expect(decisions.getByRole("link", { name: /^Approved .* for storefront/ })).toHaveCount(2)
  await expect(decisions.getByRole("link", { name: /^Approved .* for billing/ })).toHaveCount(1)
  await expect(decisions.getByRole("link", { name: /^Approved .* for deliveries/ })).toHaveCount(1)
  await expect(
    decisions.getByRole("link", { name: /^Acknowledged run .* of deliveries/ })
  ).toHaveCount(1)
  await expect(decisions.getByText(fact("LEAD"))).toHaveCount(0)
  await console.caption(
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
  await expect(page.getByLabel("Approved SQL")).toContainText(/note/)
  await console.caption(
    "The exact SQL that was approved, read from the plan the operator still keeps.",
    5000
  )
})
