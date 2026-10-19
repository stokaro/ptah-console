import { expect, test } from "@playwright/test"

const link = process.env.DEMO_LINK ?? ""
const namespace = process.env.DEMO_NAMESPACE ?? ""
const outsider = process.env.DEMO_OUTSIDER ?? ""

// An identity with no grant in the namespace is told the namespace is not
// shown because it may not read it, never shown an empty namespace, and the
// API says forbidden rather than nothing.
test("an outsider is told the namespace is not readable, not that it is empty", async ({
  page,
}) => {
  expect(link, "DEMO_LINK names the sign-in link ptah-console printed").toMatch(/^http:\/\/.+#t=/)

  await page.goto(link)
  await expect(page.getByText(outsider).first()).toBeVisible()
  await expect(page.getByText("Not shown, because this identity may not read them:")).toBeVisible()
  await expect(page.getByText(namespace, { exact: true }).first()).toBeVisible()

  const response = await page.request.post(
    `${new URL(link).origin}/api/ptah.console.v1.ScopeService/ListNamespaces`,
    { data: {}, headers: { "Content-Type": "application/json", "Connect-Protocol-Version": "1" } }
  )
  expect(response.status()).toBe(200)
  const { namespaces } = await response.json()
  expect(namespaces).toEqual([
    expect.objectContaining({ name: namespace, access: "ACCESS_FORBIDDEN" }),
  ])
})
