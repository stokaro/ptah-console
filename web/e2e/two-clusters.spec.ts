import { readFileSync } from "node:fs"
import type { Page } from "@playwright/test"

import { act, expect, fact, outPath, test } from "./lab"

// The two-cluster rows of stokaro/ptah-console#102: the gates of
// stokaro/ptah-operator#588 section 20, each walked to the state it describes
// and held to a control. The hosted console runs in the lab with the
// registry and the agent listener; lab-2 is a second kind cluster with the
// operator and no setup for the console, which the console reaches only
// through the agent its administrator installs (hack/e2e/external.sh). The
// rows run after the hosted rows, in this order, each from the state the one
// before left.

const name = "lab-2"
const enrollmentFile = outPath(`${name}.enroll`)
let connectionUid = ""

// The console serves certificates hosted.sh issued for localhost and for the
// agent listener's name.
test.use({ ignoreHTTPSErrors: true })

test.beforeAll(async () => {
  await act("external", "up")
})

/** Posts to one of the console's procedures, for a connection when one is named. */
async function call(page: Page, origin: string, procedure: string, data: unknown, connection = "") {
  const response = await page.request.post(`${origin}/api/ptah.console.v1.${procedure}`, {
    data,
    headers: {
      "Content-Type": "application/json",
      "Connect-Protocol-Version": "1",
      ...(connection ? { "Ptah-Connection": connection } : {}),
    },
  })
  return { status: response.status(), body: await response.json() }
}

/** Switches the page to another cluster through the switcher. */
async function switchTo(page: Page, cluster: string) {
  await page.getByRole("button", { name: /^Cluster: / }).click()
  await page.getByRole("menuitem", { name: new RegExp(cluster) }).click()
  await expect(page.getByRole("button", { name: new RegExp(`^Cluster: ${cluster}`) })).toBeVisible()
}

/** Reloads a page until what it waits for is on it. */
async function untilShown(page: Page, check: () => Promise<void>, timeout = 180_000) {
  await expect(async () => {
    await page.reload()
    await check()
  }).toPass({ timeout, intervals: [2_000, 5_000] })
}

/** Opens the connection's page as an installation administrator. */
async function openConnection(page: Page) {
  await page.goto(`${fact("HOSTED_URL")}/connections/${name}`)
  await expect(page.getByRole("heading", { level: 1, name: new RegExp(name) })).toBeVisible()
}

test("an administrator enrolls a second cluster, which reaches the console only through its agent", async ({
  page,
  lab,
}) => {
  const namespace = fact("NAMESPACE")
  await lab.signInHosted("admin")
  await page.getByRole("link", { name: "Admin", exact: true }).click()
  await page.getByRole("link", { name: "Add cluster" }).click()
  await expect(page.getByText(/A compromised agent can use those accounts' rights/)).toBeVisible()
  await lab.audit("Add cluster")
  await page.getByLabel("Name", { exact: true }).fill(name)
  await page.getByLabel("Namespaces it covers, separated by commas").fill(namespace)
  await page
    .getByLabel("The operator's approver ClusterRole there")
    .fill(fact("APPROVER_CLUSTER_ROLE"))
  const first = page.getByRole("group", { name: "Account 1" })
  await first.getByLabel("Account ID").fill("author")
  await first.getByRole("checkbox", { name: "approver", exact: true }).check()
  await page.getByRole("button", { name: "Add an account" }).click()
  await page.getByRole("group", { name: "Account 2" }).getByLabel("Account ID").fill("approver")
  await lab.caption(
    "Ada adds lab-2: one namespace, Avery as viewer and approver there, Arden as viewer only. lab-2's API server knows nothing of the console.",
    5000
  )
  await page.getByRole("button", { name: "Create the enrollment" }).click()
  const downloading = page.waitForEvent("download")
  await page.getByRole("button", { name: `Download ${name}.enroll` }).click()
  await (await downloading).saveAs(enrollmentFile)
  connectionUid =
    /connectionUID: "?([0-9a-f-]{36})/.exec(readFileSync(enrollmentFile, "utf8"))?.[1] ?? ""
  expect(connectionUid).toMatch(/^[0-9a-f-]{36}$/)
  const copy = page.getByRole("button", { name: /^Copy fingerprint / })
  const shown = ((await copy.getAttribute("aria-label")) ?? "").replace("Copy fingerprint ", "")
  expect(shown).toMatch(/^sha256:[0-9a-f]{64}$/)

  // A helper run that stops part way, here at writing the values, leaves
  // the enrollment completed and resumes with the same file.
  const stopped = await lab.skip(() =>
    act("external", "refused", "enroll", enrollmentFile, "/nonexistent/values.yaml")
  )
  expect(stopped).toContain(`Fingerprint     ${shown}`)
  expect(stopped).toContain("/nonexistent/values.yaml")
  const resumed = await lab.skip(() => act("external", "enroll", enrollmentFile))
  expect(resumed).toContain(`Fingerprint     ${shown}`)
  expect(resumed).toContain("The console took the enrollment.")
  await page.getByRole("checkbox", { name: "It matches what the helper printed." }).check()
  await lab.caption(
    "On lab-2, its administrator ran the helper with their own kubeconfig. It printed the same fingerprint, and ran again after it stopped part way."
  )

  // The same file on another cluster: a stolen file enrolls nothing.
  const stolen = await lab.skip(() => act("external", "refused", "replay", enrollmentFile))
  expect(stolen).toContain("already completed with another target or credential")

  await page.getByRole("button", { name: "Done" }).click()
  await lab.skip(() => act("external", "install"))
  const pending = page.getByRole("listitem").filter({ hasText: name })
  await lab.skip(() =>
    untilShown(page, () =>
      expect(
        pending.getByText("The agent is connected. Activate the connection to use it.")
      ).toBeVisible({
        timeout: 2_000,
      })
    )
  )
  await lab.caption(
    "The agent dialed the console from lab-2. Nothing is sent to it before Ada activates the connection."
  )
  await page.getByRole("button", { name: `Activate ${name}` }).click()

  const list = page.getByRole("list", { name: "Connections" })
  await expect(list.getByRole("link", { name: new RegExp(`^${name} · `) })).toBeVisible({
    timeout: 60_000,
  })
  await openConnection(page)
  await expect(page.getByText(/account "admin" is not on this agent's allowlist/)).toBeVisible({
    timeout: 60_000,
  })
  await lab.audit("a connection's page")
  await lab.caption(
    "lab-2 is connected. Ada administers the console, yet lab-2's agent was not told to act for her, so she reads nothing there."
  )

  await switchTo(page, "this-cluster")
  await page.getByRole("link", { name: "Developer", exact: true }).click()
  await expect(page.getByText("storefront").first()).toBeVisible({ timeout: 60_000 })
  await lab.caption("The cluster the console runs in is served as before.")
})

test("one account approves on one cluster and is a viewer on the other", async ({ page, lab }) => {
  const author = fact("HOSTED_AUTHOR")
  await lab.signInHosted("author")
  await page.getByRole("link", { name: "Approver", exact: true }).click()
  await expect(page.getByText("no approval verb in these namespaces")).toBeVisible({
    timeout: 60_000,
  })
  await lab.caption("In the cluster the console runs in, Avery has no approval verb.")

  await switchTo(page, name)
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const inventory = queue.getByRole("listitem").filter({ hasText: "for inventory" })
  await expect(inventory).toBeVisible({ timeout: 60_000 })
  await lab.audit("the approver queue of an external cluster")
  await lab.caption("In lab-2, inventory's first plan waits, and there Avery may approve it.")
  await inventory.getByRole("link", { name: "Review" }).click()
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 60_000 })
  await expect(
    dialog.getByText(`recorded by admission as ${author}`, { exact: false })
  ).toBeVisible()
  await lab.caption(
    `lab-2's own admission recorded ${author}: the agent acted as that username, not as itself.`
  )
  expect(await act("external", "approvers", "inventory")).toBe(author)
  await lab.skip(() => act("external", "insync", "inventory"))

  await lab.signInHosted("approver")
  await switchTo(page, name)
  await page.getByRole("link", { name: "Approver", exact: true }).click()
  await expect(page.getByText("no approval verb in these namespaces")).toBeVisible({
    timeout: 60_000,
  })
  await lab.caption("Arden approves in the cluster the console runs in, and only views lab-2.")
})

test("an account the agent was never told of reaches nothing on the target", async ({
  page,
  lab,
}) => {
  await lab.skip(() => act("hosted", "newcomer"))
  await lab.signInHosted("newcomer")
  await expect(page.getByRole("button", { name: /^Cluster: / })).toHaveCount(0)
  const { status, body } = await call(
    page,
    lab.origin(),
    "ScopeService/ListNamespaces",
    {},
    connectionUid
  )
  expect(status).toBe(404)
  expect(body.message).toContain("no connection with that UID is visible to you")
  await lab.caption(
    "Noor's account was added after lab-2 was enrolled. lab-2 is not offered to her, and a request naming it is refused."
  )

  await lab.signInHosted("author")
  const outside = await call(
    page,
    lab.origin(),
    "SchemaService/ListSchemas",
    { namespaces: ["kube-system"] },
    connectionUid
  )
  expect(outside.status).toBe(403)
  expect(outside.body.message).toContain(
    'namespace "kube-system" is outside what this agent serves'
  )
  const inside = await call(
    page,
    lab.origin(),
    "SchemaService/ListSchemas",
    { namespaces: [fact("NAMESPACE")] },
    connectionUid
  )
  expect(inside.status).toBe(200)
  await lab.caption("Even Avery is refused outside the namespaces lab-2's administrator granted.")
})

test("the agent's channel takes no credential, binding or protocol but its own", async () => {
  expect(await act("external", "probe", "garbage", "-connection", connectionUid)).toBe(
    "refused unauthenticated: the credential is not one this console accepts"
  )
  expect(
    await act("external", "probe", `bootstrap:${enrollmentFile}`, "-connection", connectionUid)
  ).toBe("refused unauthenticated: the credential is not one this console accepts")
  const other = "00000000-0000-4000-8000-0000000000aa"
  expect(await act("external", "probe", "credential", "-connection", other)).toBe(
    `refused permission_denied: the agent bound its channel to connection ${other} generation 1; its credential is for ${connectionUid} generation 1`
  )
  expect(
    await act("external", "probe", "credential", "-connection", connectionUid, "-protocol", "2")
  ).toBe("refused failed_precondition: the agent has to open with a hello for protocol 1")
  const misuse = await act("external", "misuse")
  expect(misuse).toContain("console api: 401")
  expect(misuse).toContain("listener decision: 404")

  // The agent may impersonate the allowlisted usernames and nothing else.
  expect(await act("external", "can-i", "impersonate", "users/ptah-console:e2e:author")).toBe("yes")
  for (const denied of [
    ["impersonate", "users/ptah-console:e2e:admin"],
    ["impersonate", "groups/system:masters"],
    ["create", "clusterrolebindings"],
    ["bind", "clusterroles"],
    ["escalate", "roles", "--namespace", fact("NAMESPACE")],
    ["get", "secrets", "--namespace", "ptah-console-agent"],
  ]) {
    expect(await act("external", "can-i", ...denied), denied.join(" ")).toBe("no")
  }
})

test("a restart on either side keeps what the target granted", async ({ page, lab }) => {
  await lab.skip(() => act("hosted", "restart"))
  await lab.skip(() => act("external", "restart"))
  await lab.signInHosted("author")
  await switchTo(page, name)
  const { body } = await call(page, lab.origin(), "ScopeService/ListNamespaces", {}, connectionUid)
  expect(body.namespaces).toEqual([
    expect.objectContaining({
      name: fact("NAMESPACE"),
      capabilities: expect.objectContaining({ approveSchemaPlans: "ANSWER_ALLOWED" }),
    }),
  ])
  await lab.caption("Both the console and lab-2's agent restarted. Avery still approves in lab-2.")
})

test("the agent refuses a console that is not the one it was enrolled with", async ({
  page,
  lab,
}) => {
  await lab.skip(() => act("external", "set", "--set", "console.serverName=elsewhere.e2e.test"))
  await lab.signInHosted("admin")
  await lab.skip(() =>
    untilShown(page, async () => {
      await openConnection(page)
      await expect(page.getByText("Disconnected").first()).toBeVisible({ timeout: 2_000 })
    })
  )
  expect(await act("external", "logs")).toContain("elsewhere.e2e.test")
  await lab.caption(
    "Told to expect another name, the agent refuses the console's certificate and sends nothing."
  )
  await lab.skip(() =>
    act("external", "set", "--set", "console.serverName=" + fact("AGENTS_SERVER_NAME"))
  )
  await lab.skip(() =>
    untilShown(page, async () => {
      await openConnection(page)
      await expect(page.getByText(/^Connected/).first()).toBeVisible({ timeout: 2_000 })
    })
  )
})

test("a new credential replaces the old one without a gap", async ({ page, lab }) => {
  await lab.signInHosted("admin")
  await openConnection(page)
  await page.getByRole("button", { name: "Issue a new credential…" }).click()
  await page.getByRole("alertdialog").getByRole("button", { name: "Issue" }).click()
  const command = await page.getByText(/--from-literal=credential=/).textContent()
  const credential = /--from-literal=credential=(\S+)/.exec(command ?? "")?.[1] ?? ""
  expect(credential).not.toBe("")
  await lab.caption(
    "Ada issued a second credential, shown this once. lab-2's administrator writes it into the agent's Secret."
  )
  await lab.skip(() => act("external", "credential", credential))
  await expect
    .poll(() => act("external", "credentials", name), { timeout: 240_000, intervals: [5_000] })
    .toBe("generation 1 credentials 2 used 2 retiring 1")
  await lab.caption(
    "The agent opened its channel with the new credential. The old one retires within the hour."
  )
})

test("a frozen target gets no decision late, and a fresh one goes through", async ({
  page,
  lab,
}) => {
  const plan = await lab.skip(() => act("external", "publish", "inventory-v2"))
  await lab.signInHosted("author")
  await switchTo(page, name)
  await page.getByRole("link", { name: "Approver", exact: true }).click()
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  await queue
    .getByRole("listitem")
    .filter({ hasText: "for inventory" })
    .getByRole("link", { name: "Review" })
    .click()
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()

  await act("external", "offline")
  const sent = Date.now()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  // The channel ends when lab-2 misses the console's ping, or the call's own
  // deadline passes first; either way nothing claims the decision was made.
  await expect(dialog.getByText(/may or may not have been made|did not answer/)).toBeVisible({
    timeout: 90_000,
  })
  await expect(dialog.getByText("Approval admitted")).toHaveCount(0)
  await lab.caption(
    "lab-2 froze just as Avery approved. The console cannot tell whether the decision arrived, and says so."
  )
  await page.keyboard.press("Escape")
  await untilShown(page, () =>
    expect(page.getByText(/lab-2 is not connected/)).toBeVisible({ timeout: 2_000 })
  )
  // Frozen past the request's deadline from when it was sent, and the
  // clocks' allowance: whatever waited in a buffer is stale when lab-2 reads it.
  await lab.skip(async () => {
    await page.waitForTimeout(Math.max(0, sent + 60_000 - Date.now()))
    await act("external", "online")
  })
  await lab.skip(() =>
    untilShown(page, async () => {
      await expect(page.getByRole("button", { name: "Approve this plan…" })).toBeEnabled({
        timeout: 2_000,
      })
      await expect(page.getByText(/lab-2 is not connected/)).toHaveCount(0)
    })
  )
  expect(await act("external", "approvers", "inventory", plan)).toBe("")
  await lab.caption(
    "lab-2 is back. The approval it may have had in a buffer was refused as late: nothing was approved."
  )

  await page.getByRole("button", { name: "Approve this plan…" }).click()
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 60_000 })
  expect(await act("external", "approvers", "inventory", plan)).toBe(fact("HOSTED_AUTHOR"))
  await lab.caption("Approved again, now that lab-2 answers. One approval, made once.")
  await lab.skip(() => act("external", "insync", "inventory"))
})

test("the registry refuses through kubectl what the console's pages never offer", async () => {
  const said = await act("external", "direct", name, connectionUid)
  expect(said).toMatch(
    /^admin-edits-trust: refused: .*a connection's trust and reach are immutable/m
  )
  expect(said).toMatch(/^admin-names-it: admitted$/m)
  expect(said).toMatch(/^admin-writes-a-revocation: refused: .*forbidden/m)
  expect(said).toMatch(/^admin-writes-status: refused: .*forbidden/m)
  expect(said).toMatch(/^author-adds-a-connection: refused: .*forbidden/m)
  expect(said).toMatch(/^author-reads-it: admitted$/m)
  expect(said).toMatch(/^newcomer-reads-it: refused: .*forbidden/m)
})

test("a quarantine holds when the entry is applied again, and a new generation lifts it", async ({
  page,
  lab,
}) => {
  await act("external", "save", name)
  await lab.signInHosted("admin")
  await openConnection(page)
  await page.getByRole("button", { name: "Quarantine…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog
    .getByLabel("Reason, for the record")
    .fill("acceptance: the credential may have leaked")
  await dialog.getByRole("button", { name: "Quarantine" }).click()
  await expect(page.getByText("Quarantined").first()).toBeVisible({ timeout: 60_000 })
  await lab.caption("Ada quarantined lab-2. Its channel is closed and its credentials are refused.")

  // As GitOps or a restore would: the entry deleted and created again from
  // what was kept, its recorded credentials included.
  await act("external", "drop", name)
  await act("external", "restore", name)
  expect(await act("external", "probe", "credential", "-connection", connectionUid)).toBe(
    "refused unauthenticated: the credential is not one this console accepts"
  )
  await untilShown(page, async () => {
    await openConnection(page)
    await expect(page.getByText("Quarantined").first()).toBeVisible({ timeout: 2_000 })
  })
  await lab.caption(
    "The entry came back from its copy, recorded credentials and all. The quarantine still holds."
  )

  await page.getByRole("button", { name: "Lift the quarantine…" }).click()
  await page.getByRole("alertdialog").getByRole("button", { name: "Lift" }).click()
  await expect(page.getByText(/shown this once/)).toHaveText(/with generation 2/)
  const command = await page.getByText(/--from-literal=credential=/).textContent()
  const credential = /--from-literal=credential=(\S+)/.exec(command ?? "")?.[1] ?? ""
  await lab.skip(async () => {
    await act("external", "credential", credential)
    await act("external", "set", "--set", "connection.generation=2")
  })
  await untilShown(page, async () => {
    await openConnection(page)
    await expect(page.getByText(/generation 2/)).toBeVisible({ timeout: 2_000 })
    await expect(page.getByText("Quarantined")).toHaveCount(0)
  })
  await lab.caption("A credential at generation 2, installed on lab-2, lifted the quarantine.")
})

test("removing an offline target revokes it at once and leaves its cleanup to its administrator", async ({
  page,
  lab,
}) => {
  const before = await act("external", "untouched")
  await act("external", "save", name)
  await lab.signInHosted("admin")
  await openConnection(page)
  await act("external", "offline")
  await page.getByRole("button", { name: "Remove…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByLabel("Reason, for the record").fill("acceptance: lab-2 is retired")
  await dialog.getByRole("button", { name: "Remove" }).click()
  await expect(page.getByText(/The cleanup on the target is pending/)).toBeVisible({
    timeout: 60_000,
  })
  await expect(page.getByText(/helm uninstall ptah-console-agent/)).toBeVisible()
  await lab.audit("a removed connection")
  await lab.caption(
    "lab-2 was offline. Ada removed it anyway: its credentials are refused, and the cleanup on lab-2 waits."
  )

  await lab.skip(() => act("external", "online"))
  expect(
    await act("external", "probe", "credential", "-connection", connectionUid, "-generation", "2")
  ).toBe("refused unauthenticated: the credential is not one this console accepts")
  await act("external", "restore", name)
  expect(
    await act("external", "probe", "credential", "-connection", connectionUid, "-generation", "2")
  ).toBe("refused unauthenticated: the credential is not one this console accepts")
  await act("external", "drop", name)
  await lab.caption(
    "Back online, the agent is refused, and an entry restored from a copy does not revive it."
  )

  // Nothing of the operator, its decisions or the database was touched.
  await expect.poll(() => act("external", "untouched"), { timeout: 120_000 }).toBe(before)

  // The name is free again, and a new enrollment of it is another connection.
  await page.goto(`${fact("HOSTED_URL")}/connections/new`)
  await page.getByLabel("Name", { exact: true }).fill(name)
  await page.getByLabel("Namespaces it covers, separated by commas").fill(fact("NAMESPACE"))
  await page
    .getByLabel("The operator's approver ClusterRole there")
    .fill(fact("APPROVER_CLUSTER_ROLE"))
  await page.getByRole("group", { name: "Account 1" }).getByLabel("Account ID").fill("author")
  await page.getByRole("button", { name: "Create the enrollment" }).click()
  const downloading = page.waitForEvent("download")
  await page.getByRole("button", { name: `Download ${name}.enroll` }).click()
  const again = outPath(`${name}-again.enroll`)
  await (await downloading).saveAs(again)
  const reused = /connectionUID: "?([0-9a-f-]{36})/.exec(readFileSync(again, "utf8"))?.[1] ?? ""
  expect(reused).toMatch(/^[0-9a-f-]{36}$/)
  expect(reused).not.toBe(connectionUid)
  await lab.caption(
    "lab-2's name was reused: the new enrollment is a new connection, which the old agent's credential does not open."
  )

  const cleanup = await lab.skip(() => act("external", "unenroll", connectionUid))
  expect(cleanup).toContain("The credential Secret is gone.")
})
