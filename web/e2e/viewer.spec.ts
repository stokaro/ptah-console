import { expect, test } from "@playwright/test"

const link = process.env.DEMO_LINK ?? ""
const viewer = process.env.DEMO_VIEWER ?? ""

// The viewer may read schemas and plans and not the chunks a plan's SQL is
// stored in. The page shows the plan's manifest, says the SQL is not granted
// rather than showing an empty plan, and offers no approval.
test("a viewer sees the plan without its SQL and is not offered approval", async ({ page }) => {
  expect(link, "DEMO_LINK names the sign-in link ptah-console printed").toMatch(/^http:\/\/.+#t=/)

  await page.goto(link)
  await expect(page.getByText(viewer).first()).toBeVisible()
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const planLink = queue
    .getByRole("listitem")
    .filter({ hasText: "for storefront" })
    .getByRole("link", { name: /^ptah-plan-/ })
  await expect(planLink).toBeVisible({ timeout: 5 * 60_000 })
  await expect(queue.getByRole("button", { name: "Approve…" })).toHaveCount(0)

  await queue.getByRole("link", { name: "Review" }).click()
  await expect(page.getByText("You may not read this plan's SQL")).toBeVisible()
  await expect(
    page.getByText("This identity may read the plan but not the chunks its SQL is stored in", {
      exact: false,
    })
  ).toBeVisible()
  await expect(page.getByRole("list", { name: "SQL statements" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Approve this plan…" })).toHaveCount(0)
})
