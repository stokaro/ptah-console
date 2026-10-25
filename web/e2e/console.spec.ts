import type { Page } from "@playwright/test"

import { act, expect, fact, test } from "./lab"

// The rows for the full console's pages and actions that came after the
// first acceptance rows. They run after acceptance.spec.ts, in this order, and
// before the hosted rows: the author creates catalog and ledger through the
// console's form, asks inventory to read its database now, and suspends and
// resumes catalog; the approver approves both new plans from the full
// console's plan review; on call reads journal through Execution & diagnosis
// and Activity. hack/e2e/verify.sh then checks the cluster's account of them.

const namespace = fact("NAMESPACE")
// How long the operator may take to apply a plan and verify it.
const applyTimeout = 15 * 60_000
// How long a plan may take to appear, when the operator plans again.
const planTimeout = 5 * 60_000

/** Goes from Focus, where a console starts, to the full console. */
async function fullConsole(page: Page) {
  await page.getByRole("link", { name: "Full console" }).click()
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible()
}

/** Opens a resource's page through the full console's palette. */
async function open(page: Page, kind: "PtahSchema" | "PtahMigration", name: string) {
  await page.keyboard.press("ControlOrMeta+k")
  await page.getByRole("combobox", { name: "Search pages and resources" }).fill(name)
  await page.keyboard.press("Enter")
  await expect(
    page.getByRole("heading", { level: 1, name: new RegExp(`${kind} / ${name}`) })
  ).toBeVisible()
}

test("an author creates a schema from a ConfigMap, checked by the API server first", async ({
  page,
  lab,
}) => {
  await lab.signIn("author")
  await fullConsole(page)
  await page
    .getByRole("navigation", { name: "Console" })
    .getByRole("link", { name: /^Resources/ })
    .click()
  await page.getByRole("link", { name: "New schema" }).click()
  const form = page.getByRole("form", { name: "New PtahSchema" })
  await form
    .getByRole("group", { name: "Source" })
    .getByRole("button", { name: "A ConfigMap" })
    .click()
  await form.getByLabel("Name", { exact: true }).fill("catalog")
  await form.getByLabel("Coordination key", { exact: true }).fill("e2e/catalog")
  await form.getByLabel("Connection Secret", { exact: true }).fill("e2e-database-catalog")
  await form.getByLabel("ConfigMap", { exact: true }).fill("catalog-schema")
  await form.getByLabel("Key", { exact: true }).fill("schema.sql")
  await form.getByLabel("File name", { exact: true }).fill("schema.sql")
  await lab.audit("a new schema, filled in")
  await lab.caption(
    "The author fills in typed fields: the database by coordination key and Secret reference, the desired schema as one file of a ConfigMap. The console builds the object from them.",
    5500
  )

  await page.getByRole("button", { name: "Check with the API server" }).click()
  await expect(page.getByText("The API server accepts this PtahSchema")).toBeVisible({
    timeout: 30_000,
  })
  const checked = page.getByLabel("What would be created")
  await expect(checked).toContainText("configMap")
  await expect(checked).toContainText("apply: OnApproval")
  await lab.audit("a new schema the API server checked")
  await lab.caption(
    "A server-side dry run: the CRD's schema and the operator's admission judge the object and fill in its defaults, and nothing is stored.",
    5000
  )

  await page.getByRole("button", { name: "Create PtahSchema" }).click()
  await expect(page.getByText("Created PtahSchema catalog")).toBeVisible({ timeout: 30_000 })
  await page.getByRole("link", { name: `Open ${namespace}/catalog` }).click()
  await expect(page.getByRole("heading", { level: 1, name: /PtahSchema \/ catalog/ })).toBeVisible()
  const plan = page.getByRole("region", { name: "Current plan" })
  await lab.skip(() => expect(plan).toContainText(/ptah-plan-/, { timeout: planTimeout }))
  await expect(plan).toContainText("Not approved")
  await lab.audit("a schema the console created, waiting for approval")
  await lab.caption(
    "Created as the author. The operator read the ConfigMap, planned against the empty database, and waits for an approver.",
    5000
  )
})

test("an author creates a migration from the files of a ConfigMap", async ({ page, lab }) => {
  await lab.signIn("author")
  await fullConsole(page)
  await page.keyboard.press("ControlOrMeta+k")
  await page.getByRole("combobox", { name: "Search pages and resources" }).fill("New migration")
  await page.keyboard.press("Enter")
  const form = page.getByRole("form", { name: "New PtahMigration" })
  await form
    .getByRole("group", { name: "Source" })
    .getByRole("button", { name: "A ConfigMap" })
    .click()
  await form.getByLabel("Name", { exact: true }).fill("ledger")
  await form.getByLabel("Coordination key", { exact: true }).fill("e2e/ledger")
  await form.getByLabel("Connection Secret", { exact: true }).fill("e2e-database-ledger")
  await form.getByLabel("ConfigMap", { exact: true }).fill("ledger-migrations")
  const up = form.getByRole("group", { name: "File 1" })
  await up.getByLabel("Key", { exact: true }).fill("0000000001-up")
  await up.getByLabel("File name", { exact: true }).fill("0000000001_create_ledger_entries.up.sql")
  await form.getByRole("button", { name: "Add a file" }).click()
  const down = form.getByRole("group", { name: "File 2" })
  await down.getByLabel("Key", { exact: true }).fill("0000000001-down")
  await down
    .getByLabel("File name", { exact: true })
    .fill("0000000001_create_ledger_entries.down.sql")
  await lab.audit("a new migration from the files of a ConfigMap")
  await page.getByRole("button", { name: "Check with the API server" }).click()
  await expect(page.getByText("The API server accepts this PtahMigration")).toBeVisible({
    timeout: 30_000,
  })
  await page.getByRole("button", { name: "Create PtahMigration" }).click()
  await expect(page.getByText("Created PtahMigration ledger")).toBeVisible({ timeout: 30_000 })
  await page.getByRole("link", { name: `Open ${namespace}/ledger` }).click()
  await expect(
    page.getByRole("heading", { level: 1, name: /PtahMigration \/ ledger/ })
  ).toBeVisible()
  await lab.skip(() =>
    expect(page.getByRole("link", { name: "Review plan" })).toBeVisible({ timeout: planTimeout })
  )
  await lab.audit("a migration the console created, waiting for approval")
  await lab.caption(
    "The same form makes a PtahMigration from several files of a ConfigMap, each key the file Ptah reads it as. The operator planned the migration in them.",
    5000
  )
})

test("an author asks inventory to read its database now, and the operator reads it before its interval would", async ({
  page,
  lab,
}) => {
  // The read the interval would start next, far enough off that the one
  // asked for has to finish first.
  const scheduled = await act("next-read", "inventory")
  await lab.signIn("author")
  await fullConsole(page)
  await open(page, "PtahSchema", "inventory")
  await lab.caption(
    "inventory is in sync, and its next read is minutes away. The author asks for one now.",
    4000
  )

  await page.getByRole("button", { name: "Actions" }).click()
  await page.getByRole("menuitem", { name: "Read again now" }).click()
  await expect(page.getByText(/A read was asked .+, and the operator started it\./)).toBeVisible({
    timeout: planTimeout,
  })
  await lab.audit("a read the operator started on request")
  await lab.caption(
    "The console set one annotation on inventory, as the author, and the operator recorded the request as answered."
  )
  await lab.skip(() => act("read-done", "inventory", scheduled))
  await lab.caption(
    "The operator resolved, read the database and found it in sync, before the interval's own read was due.",
    4500
  )
})

test("an approver reviews the new plans in the full console, approves them, and follows them to the database", async ({
  page,
  lab,
}) => {
  const approver = fact("APPROVER")
  await lab.signIn("approver")
  await fullConsole(page)
  await open(page, "PtahSchema", "catalog")
  await page.getByRole("link", { name: "Review plan" }).click()
  await expect(page.getByRole("heading", { level: 1, name: "Plan review" })).toBeVisible()
  await expect(page.getByRole("region", { name: "Integrity" })).toContainText("Verified")
  await expect(page.getByRole("list", { name: "SQL statements" })).toContainText(
    /CREATE TABLE .*products/i
  )
  const checks = page.getByRole("list", { name: "Checks before approving" })
  await expect(checks).toBeVisible()
  await expect(checks).not.toContainText("may not")
  await lab.audit("the full console's plan review")
  await lab.caption(
    "The full console's plan review: the statement types the plan reports, its integrity, the exact SQL, and the checks before approving.",
    5500
  )

  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await expect(
    dialog.getByText(`recorded by admission as ${approver}`, { exact: false })
  ).toBeVisible()
  await dialog.getByRole("link", { name: "Follow it" }).click()
  await expect(page.getByRole("heading", { level: 1, name: /PtahSchema \/ catalog/ })).toBeVisible()
  await lab.skip(() =>
    expect(
      page
        .getByRole("list", { name: "Progress of catalog" })
        .getByText("Verify: done", { exact: true })
    ).toBeVisible({ timeout: applyTimeout })
  )
  await lab.caption(
    "Approved as the approver, from the full console, and followed on its own page: applied, and verified by a fresh read.",
    4500
  )
  // The read that verified catalog counted its objects on both sides: the
  // one table and its three columns, declared and held.
  for (const side of ["Objects declared in the schema", "Objects found in the database"]) {
    await expect(page.getByRole("list", { name: side }).getByRole("listitem")).toHaveText([
      "Columns3",
      "Tables1",
    ])
  }
  await expect(
    page.getByText("No object differs between the schema and the database.")
  ).toBeVisible()
  await lab.audit("a schema's desired and observed state")
  await lab.caption(
    "The same read counted catalog's objects on both sides: what the schema declares, what the database holds, and nothing that differs.",
    4500
  )

  await page.getByRole("link", { name: "Execution and history" }).click()
  const history = page.getByRole("table", { name: /Execution and history of catalog/ })
  await expect(history).toContainText("Plan published")
  await expect(history).toContainText("Apply succeeded")
  await expect(history).toContainText("Verified in sync")
  await lab.audit("the history of a schema the console created")
  await lab.caption(
    "Its history, from the journal the operator keeps in status: the plan, the Apply and the verification.",
    4500
  )

  await open(page, "PtahMigration", "ledger")
  await page.getByRole("link", { name: "Review plan" }).click()
  const steps = page.getByRole("table", { name: "Steps in order" })
  await expect(steps.getByRole("cell", { name: /^0*1$/ })).toBeVisible()
  await expect(page.getByText("SQL is not available through this operator API")).toBeVisible()
  await lab.audit("the full console's migration plan")
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await dialog.getByRole("link", { name: "Follow it" }).click()
  await lab.skip(() =>
    expect(
      page
        .getByRole("list", { name: "Progress of ledger" })
        .getByText("Verify history: done", { exact: true })
    ).toBeVisible({ timeout: applyTimeout })
  )
  await expect(page.getByRole("list", { name: "Runs, newest first" })).toContainText(
    "Run applied 1 version"
  )
  // The revision row the run wrote, as the operator's history read found it.
  const ledgerSteps = page.getByRole("list", { name: "Migrations of ledger, newest first" })
  await expect(ledgerSteps.getByRole("listitem")).toHaveCount(1)
  // Ptah names the migration from its file, 0000000001_create_ledger_entries.
  await expect(ledgerSteps.getByRole("listitem")).toContainText("Create Ledger Entries")
  await expect(ledgerSteps.getByRole("listitem")).toContainText("applied")
  const rows = page.getByRole("table", { name: "Revision rows of ledger" })
  await expect(rows.getByRole("row")).toHaveCount(2)
  await expect(rows.getByRole("row").nth(1)).toContainText("applied")
  await lab.audit("a migration's runs and its revision rows")
  await lab.caption(
    "ledger applied its migration. The page lists the revision row it wrote and the runs the journal keeps, with how each ended and how long it took.",
    5500
  )

  await page
    .getByRole("tablist", { name: "Sections of ledger" })
    .getByRole("tab", { name: "Events" })
    .click()
  // The chart's approver role reads no Events, and the panel says so rather
  // than showing none.
  await expect(page.getByText(/^Events: not granted/)).toBeVisible()
  await lab.audit("a migration's Events, not granted")
})

test("an author suspends catalog and resumes it, and the operator follows both", async ({
  page,
  lab,
}) => {
  // A status write between reading the page and confirming moves the
  // resourceVersion the request is bound to; the scheduled read is waited
  // out first.
  await act("next-read", "catalog")
  await lab.signIn("author")
  await fullConsole(page)
  await open(page, "PtahSchema", "catalog")
  const suspended = page
    .getByRole("table", { name: "Conditions" })
    .getByRole("row")
    .filter({ has: page.getByRole("rowheader", { name: "Suspended", exact: true }) })

  await page.getByRole("button", { name: "Actions" }).click()
  await page.getByRole("menuitem", { name: "Suspend…" }).click()
  const dialog = page.getByRole("alertdialog")
  await expect(dialog).toContainText("Only spec.suspend changes")
  await lab.audit("the suspend dialog")
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Suspended", { exact: true })).toBeVisible({ timeout: 30_000 })
  await dialog.getByRole("button", { name: "Close" }).click()
  await expect(
    page.getByRole("heading", { level: 1, name: /PtahSchema \/ catalog/ }).getByText("Suspended")
  ).toBeVisible()
  await expect(suspended).toContainText("True", { timeout: planTimeout })
  await lab.caption(
    "Suspended: the console changed spec.suspend alone, and the operator reports it starts nothing until it is resumed."
  )

  await page.getByRole("button", { name: "Actions" }).click()
  await page.getByRole("menuitem", { name: "Resume…" }).click()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Resumed", { exact: true })).toBeVisible({ timeout: 30_000 })
  await dialog.getByRole("button", { name: "Close" }).click()
  await expect(suspended).toContainText("False", { timeout: planTimeout })
  await lab.skip(() => act("insync", "catalog"))
  await lab.caption("Resumed. The operator read the database again and found it in sync.", 4000)
})

test("on call reads journal's runs, its execution and diagnosis, and the Events in Activity", async ({
  page,
  lab,
}) => {
  await lab.signIn("diag")
  await fullConsole(page)
  await open(page, "PtahMigration", "journal")
  await expect(page.getByRole("list", { name: "Runs, newest first" })).toContainText(
    /Run failed at migration 2/
  )
  await lab.caption(
    "journal's page lists its runs from the operator's journal: the failed one says where it stopped."
  )

  await page.getByRole("link", { name: "Execution & diagnosis" }).click()
  await expect(page.getByRole("heading", { level: 1, name: "Execution & diagnosis" })).toBeVisible()
  await expect(page.getByRole("list", { name: "Jobs the status names" })).toContainText(
    /ptah-m-apply-journal-/
  )
  await expect(page.getByRole("region", { name: "Kubernetes Events" })).toBeVisible()
  await expect(page.getByText(/^No blind retry, no automatic rollback\./)).toBeVisible()
  await lab.audit("execution and diagnosis of a failed run")
  await lab.caption(
    "Execution & diagnosis: the Jobs the status names, the database as last observed, the Events, and what a person may do next. Nothing is retried blindly.",
    6000
  )

  const nav = page.getByRole("navigation", { name: "Console" })
  await nav.getByRole("link", { name: "Activity" }).click()
  const entries = page.getByRole("region", { name: "Entries" })
  await expect(entries).toContainText(/Run failed at migration 2/)
  await page
    .getByRole("group", { name: "Type" })
    .getByRole("button", { name: /^Kubernetes Events/ })
    .click()
  // The author's request earlier: the operator said so in an Event.
  await expect(entries.getByText(/ReadRequested/).first()).toBeVisible()
  await lab.audit("activity")
  await lab.caption(
    "Activity: what the journals, the decisions and the Kubernetes Events say happened across the namespace, filtered here to Events.",
    5000
  )

  await nav.getByRole("link", { name: "Architecture & safety" }).click()
  await expect(page.getByRole("region", { name: "System map" })).toBeVisible()
  await expect(page.getByRole("region", { name: "Security boundaries" })).toBeVisible()
  await lab.audit("architecture and safety")
  await lab.caption(
    "Architecture & safety: how the operator, its Jobs and the console fit together, and the boundaries each holds.",
    4500
  )
})
