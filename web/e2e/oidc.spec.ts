import { act, expect, fact, test } from "./lab"

// OpenID Connect sign-in, after the hosted rows: hack/e2e/oidc.sh starts a
// Dex every API server of the lab trusts, and installs the chart again with
// oidc. The person signs in at Dex and acts in Kubernetes with their own ID
// token, so admission records the username the API server maps the token to,
// and the console impersonates nobody. hack/e2e/verify.sh checks the
// approval against the cluster.

// Dex's Service name, which the browser reaches through a port-forward on this
// machine: the issuer has to read the same to the browser, the console's Pod
// and the API servers.
const dexHost = "dex.ptah-oidc.svc.cluster.local"
// How long the operator may take to apply a plan and verify it.
const applyTimeout = 15 * 60_000

test.use({
  ignoreHTTPSErrors: true,
  launchOptions: { args: [`--host-resolver-rules=MAP ${dexHost} 127.0.0.1`] },
})

test("a person signs in with OpenID Connect and approves as the username the API server gave their token", async ({
  page,
  lab,
}) => {
  const plan = await lab.skip(async () => {
    await act("oidc", "up")
    return act("catalog", "v2")
  })
  const username = fact("OIDC_USERNAME")
  await page.goto(`${fact("OIDC_URL")}/login`)
  const signIn = page.getByRole("link", { name: "Sign in with E2E Dex" })
  await expect(signIn).toBeVisible({ timeout: 60_000 })
  await lab.audit("the sign-in page of a console that signs in with OpenID Connect")
  await lab.caption(
    "This console signs people in with an OpenID Connect provider, and offers no password form of its own."
  )

  await signIn.click()
  await page.locator("#login").fill(fact("OIDC_EMAIL"))
  await page.locator("#password").fill(fact("OIDC_PASSWORD"))
  await lab.caption(
    "The provider signs the person in, and sends the browser back with a code.",
    3000
  )
  await page.locator("#submit-login").click()
  await expect(page.getByText(username).first()).toBeVisible({ timeout: 60_000 })
  await expect(page.getByText("· OIDC", { exact: false }).first()).toBeVisible()
  await lab.caption(
    `The console checked the ID token and asked the API server whom it stands for: ${username}, a username the API server mapped, not one the console impersonates.`,
    5500
  )

  const queue = page.getByRole("list", { name: "Plans waiting for approval" })
  const catalog = queue.getByRole("listitem").filter({ hasText: "for catalog" })
  await expect(catalog.getByRole("link", { name: plan })).toBeVisible({ timeout: 60_000 })
  await catalog.getByRole("link", { name: "Review" }).click()
  await expect(page.getByRole("list", { name: "SQL statements" })).toContainText(/sku/i)
  await lab.audit("a plan reviewed by an OpenID Connect session")
  await page.getByRole("button", { name: "Approve this plan…" }).click()
  const dialog = page.getByRole("alertdialog")
  await dialog.getByRole("checkbox", { name: "I reviewed what this changes." }).check()
  await dialog.getByRole("button", { name: "Confirm" }).click()
  await expect(dialog.getByText("Approval admitted")).toBeVisible({ timeout: 30_000 })
  await expect(
    dialog.getByText(`recorded by admission as ${username}`, { exact: false })
  ).toBeVisible()
  await lab.caption(
    "Admission recorded the username the token stands for. The person's own token sent the approval.",
    5000
  )

  await dialog.getByRole("link", { name: "Follow it" }).click()
  await lab.skip(() =>
    expect(
      page
        .getByRole("list", { name: "Progress of catalog" })
        .getByText("Verify: done", { exact: true })
    ).toBeVisible({ timeout: applyTimeout })
  )
  await lab.caption("catalog v2 reached its database and was verified by a fresh read.", 4000)
  await lab.skip(() => act("oidc", "down"))
})
