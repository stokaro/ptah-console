import { expect, test } from "@playwright/test"

const link = process.env.DEMO_LINK ?? ""
const namespace = process.env.DEMO_NAMESPACE ?? ""
const author = process.env.DEMO_AUTHOR ?? ""

// The author may edit the schema and read its plan, and RBAC gives them no
// approval verb. The page does not offer one; an approval sent anyway, the
// way a script would through the same session, is refused by the API
// server's own RBAC, and the console says so in its words.
test("an author is not offered approval, and one sent anyway is forbidden", async ({ page }) => {
  expect(link, "DEMO_LINK names the sign-in link ptah-console printed").toMatch(/^http:\/\/.+#t=/)
  const origin = new URL(link).origin

  await page.goto(link)
  await expect(page.getByText(author).first()).toBeVisible()
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  // The operator may be planning again when the page opens; the queue follows
  // it and shows the plan once it is published.
  const planLink = queue
    .getByRole("listitem")
    .filter({ hasText: "for storefront" })
    .getByRole("link", { name: /^ptah-plan-/ })
  await expect(planLink).toBeVisible({ timeout: 5 * 60_000 })
  const plan = (await planLink.textContent()) ?? ""
  await expect(queue.getByRole("button", { name: "Approve…" })).toHaveCount(0)
  await expect(page.getByText("no approval verb in these namespaces")).toBeVisible()

  await queue.getByRole("link", { name: "Review" }).click()
  await expect(page.getByRole("list", { name: "SQL statements" })).toBeVisible()
  await expect(page.getByText(`This identity may not approve plans in ${namespace}`)).toBeVisible()
  await expect(page.getByRole("button", { name: "Approve this plan…" })).toHaveCount(0)

  const call = async (procedure: string, data: unknown) => {
    const response = await page.request.post(`${origin}/api/ptah.console.v1.${procedure}`, {
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
