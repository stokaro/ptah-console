import { act, expect, fact, test } from "./lab"

// The hosted rows of stokaro/ptah-console#37: this repository's chart in the
// operator's lab, with an author account and an approver account, against
// inventory v2, which waits for an approval. They run after the local rows,
// in this order. hack/e2e/verify.sh then checks that admission recorded the
// approver account's own username and that the operator applied the plan.

const namespace = fact("NAMESPACE")

// The console serves a certificate hosted.sh issued for localhost.
test.use({ ignoreHTTPSErrors: true })

test.beforeAll(async () => {
  await act("hosted", "up")
})

test("an author signs in with a Console account and is not offered the approval", async ({
  page,
  lab,
}) => {
  const author = fact("HOSTED_AUTHOR")
  await lab.signInHosted("author")
  await expect(page.getByText("Avery Author")).toBeVisible()
  await lab.audit("the hosted approver queue")
  await lab.caption(
    `Installed with this repository's chart. Avery signed in with a Console account and acts as ${author}, which the console impersonates.`,
    5000
  )
  await page.getByRole("link", { name: "Approver" }).click()
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const inventory = queue.getByRole("listitem").filter({ hasText: "for inventory" })
  await expect(inventory.getByRole("link", { name: /^ptah-plan-/ })).toBeVisible()
  await expect(queue.getByRole("button", { name: "Approve…" })).toHaveCount(0)
  await expect(page.getByText("no approval verb in these namespaces")).toBeVisible()
  await lab.caption(
    "The chart bound the author's username to the author's roles: no approval verb."
  )

  const call = async (procedure: string, data: unknown) => {
    const response = await page.request.post(`${lab.origin()}/api/ptah.console.v1.${procedure}`, {
      data,
      headers: { "Content-Type": "application/json", "Connect-Protocol-Version": "1" },
    })
    expect(response.status(), `${procedure} answered`).toBe(200)
    return response.json()
  }
  const { plan } = await call("SchemaService/GetSchemaPlan", {
    namespace,
    schema: "inventory",
    selection: "PLAN_SELECTION_CURRENT",
  })
  const { result } = await call("DecisionService/ApproveSchemaPlan", {
    namespace,
    schemaName: "inventory",
    schemaUid: plan.schemaUid,
    planName: plan.name,
    planUid: plan.uid,
    planFingerprint: plan.fingerprint,
    contractVersion: plan.contractVersion,
  })
  expect(result.outcome).toBe("DECISION_OUTCOME_FORBIDDEN")
  expect(result.message).toContain(`User "${author}" cannot create resource "ptahschemaapprovals"`)
})

test("a restart of the console signs everyone out", async ({ page, lab }) => {
  await lab.signInHosted("approver")
  await expect(page.getByText("Arden Approver")).toBeVisible()
  await lab.caption("Arden, the approver account, is signed in.")

  await lab.skip(() => act("hosted", "restart"))
  await page.reload()

  await expect(page.getByRole("form", { name: "Sign in" })).toBeVisible({ timeout: 60_000 })
  await expect(page).toHaveURL(/\/login$/)
  await lab.audit("the sign-in form")
  await lab.caption(
    "The console's Pod was replaced. Sessions live in its memory, so everyone signs in again."
  )
})

test("a changed password takes effect with the rollout, and the old one stops working", async ({
  page,
  lab,
}) => {
  await lab.skip(() => act("hosted", "password", "approver"))
  await page.goto(`${fact("HOSTED_URL")}/login`)
  const form = page.getByRole("form", { name: "Sign in" })
  await form.getByLabel("Login").fill("approver@e2e.test")
  await form.getByLabel("Password").fill(fact("HOSTED_APPROVER_OLD_PASSWORD"))
  await form.getByRole("button", { name: "Sign in" }).click()
  await expect(form.getByRole("alert")).toHaveText("The login or the password is wrong.")
  await lab.caption(
    "The approver's password changed in its Secret and the console rolled out. The old password is refused, with the answer an unknown login gets."
  )

  await lab.signInHosted("approver")
  await expect(page.getByText("Arden Approver")).toBeVisible()
  await lab.caption("The new one signs in.")
})

test("an approver approves, and the operator applies the plan with the console uninstalled", async ({
  page,
  lab,
}) => {
  const approver = fact("HOSTED_APPROVER")
  await lab.signInHosted("approver")
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const inventory = queue.getByRole("listitem").filter({ hasText: "for inventory" })
  await inventory.getByRole("link", { name: "Review" }).click()
  const sql = page.getByRole("list", { name: "SQL statements" })
  await expect(sql).toContainText(/ADD COLUMN/i)
  await lab.caption("inventory v2 adds a column. The administrator wrote it; Arden reviews it.")

  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await expect(
    dialog.getByText(`recorded by admission as ${approver}`, { exact: false })
  ).toBeVisible()
  await lab.caption(
    `Approved. Admission recorded ${approver}, the username the console impersonated.`
  )

  await lab.skip(() => act("hosted", "uninstall"))
  await lab.caption(
    "The console is uninstalled. The approval is the operator's to act on, and it needs nothing from the console.",
    4500
  )
  await lab.skip(() => act("insync", "inventory"))
  await lab.caption("inventory is InSync: the operator applied the approved plan on its own.", 4000)
})
