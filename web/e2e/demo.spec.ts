import { expect, test, type Page } from "@playwright/test"

const link = process.env.DEMO_LINK ?? ""
const approver = process.env.DEMO_APPROVER ?? ""
// How long the operator may take to apply and verify the plan.
const applyTimeout = Number(process.env.DEMO_APPLY_TIMEOUT_MS ?? 15 * 60_000)

/**
 * Shows a caption at the bottom of the page for the video. It is built with
 * the DOM and CSSOM, which the page's Content Security Policy allows.
 */
async function caption(page: Page, text: string) {
  await page.evaluate((line) => {
    let box = document.getElementById("demo-caption")
    if (!box) {
      box = document.createElement("div")
      box.id = "demo-caption"
      Object.assign(box.style, {
        position: "fixed",
        left: "50%",
        top: "74px",
        transform: "translateX(-50%)",
        zIndex: "100",
        maxWidth: "860px",
        padding: "12px 18px",
        borderRadius: "14px",
        background: "rgba(12, 15, 34, 0.88)",
        color: "#fff",
        font: "500 16px/1.45 system-ui, sans-serif",
        textAlign: "center",
        pointerEvents: "none",
      })
      document.body.append(box)
    }
    box.textContent = line
  }, text)
}

test("an approver approves a plan and follows it to the database", async ({ page }) => {
  expect(link, "DEMO_LINK names the sign-in link ptah-console printed").toMatch(/^http:\/\/.+#t=/)

  await page.goto(link)
  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  // The operator may be planning again when the page opens; the queue follows
  // it and shows the plan once it is published.
  const planLink = queue
    .getByRole("listitem")
    .filter({ hasText: "for storefront" })
    .getByRole("link", { name: /^ptah-plan-/ })
  await expect(planLink).toBeVisible({ timeout: 5 * 60_000 })
  const plan = (await planLink.textContent()) ?? ""
  await expect(page.getByText(approver).first()).toBeVisible()
  await caption(
    page,
    `Signed in from the link ptah-console printed. The console acts as ${approver}, with that identity's RBAC.`
  )
  await page.waitForTimeout(4000)
  await caption(
    page,
    "The operator planned a change to the storefront schema and waits for an approver."
  )
  await page.waitForTimeout(3500)

  await queue.getByRole("link", { name: "Review" }).click()
  const sql = page.getByRole("list", { name: "SQL statements" })
  await expect(sql).toBeVisible()
  await caption(
    page,
    "The exact SQL, read through the operator's integrity-checked plan reader, shown as plain text."
  )
  await page.waitForTimeout(4500)
  await page.mouse.wheel(0, 500)
  await caption(
    page,
    "The approval binds this plan, its UID and its fingerprint. Nothing else is sent."
  )
  await page.waitForTimeout(3500)

  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await expect(dialog.getByText(approver)).toBeVisible()
  await caption(page, "The dialog says where the decision lands and whom it is sent as.")
  await page.waitForTimeout(3000)
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await page.waitForTimeout(1200)
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await expect(
    dialog.getByText(`recorded by admission as ${approver}`, { exact: false })
  ).toBeVisible()
  await caption(page, "The operator's admission stamped the approver. Approved is not yet applied.")
  await page.waitForTimeout(4000)

  await dialog.getByRole("link", { name: "Follow it" }).click()
  const progress = page.getByRole("list", { name: "Progress of storefront" })
  await expect(progress).toBeVisible()
  await caption(
    page,
    "The page follows the schema live while the operator applies the plan in a Job."
  )
  await expect(progress.getByText("Verify: done", { exact: true })).toBeVisible({
    timeout: applyTimeout,
  })
  await caption(page, "Applied, and verified by a fresh read of the database.")
  await page.waitForTimeout(5000)

  await page.getByRole("link", { name: "Ptah Console, home" }).click()
  await page.getByRole("link", { name: "Auditor" }).click()
  const decisions = page.getByRole("list", { name: "Decisions" })
  await expect(decisions.getByRole("link", { name: new RegExp(`Approved ${plan}`) })).toBeVisible()
  await caption(
    page,
    "The record: who approved which plan, as admission wrote it, and that the operator consumed it."
  )
  await page.waitForTimeout(5000)

  await page.getByRole("link", { name: "Approver" }).click()
  await expect(page.getByRole("heading", { name: "Nothing waits for you" })).toBeVisible()
  await caption(page, "Nothing waits any more.")
  await page.waitForTimeout(3500)
})
