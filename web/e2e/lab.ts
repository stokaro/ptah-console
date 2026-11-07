import AxeBuilder from "@axe-core/playwright"
import {
  test as base,
  expect,
  type Locator,
  type Page,
  type Request,
  type Route,
} from "@playwright/test"
import { execFile, spawn, type ChildProcess } from "node:child_process"
import { closeSync, openSync, readFileSync, writeFileSync } from "node:fs"
import { request as httpsRequest } from "node:https"
import path from "node:path"
import { promisify } from "node:util"

// The acceptance rows run against the environment `make e2e-up` built. Its
// scripts wrote a kubeconfig per person and the facts below into OUT_DIR.
const root = path.resolve(import.meta.dirname, "../..")
const outDir = process.env.OUT_DIR ?? path.join(root, ".e2e/out")
const binary = process.env.CONSOLE_BIN ?? path.join(root, "bin/ptah-console")
const run = promisify(execFile)

/** The people the rows sign in as; personas.sh says what each may do. */
export type Persona = "outsider" | "viewer" | "reviewer" | "author" | "lead" | "approver" | "diag"

/** The hosted console's accounts; hosted.sh says what each may do. */
export type Account = "author" | "approver" | "admin" | "newcomer"

/** One fact the environment scripts recorded, such as NAMESPACE or APPROVER. */
export function fact(name: string): string {
  const line = readFileSync(path.join(outDir, "facts"), "utf8")
    .split("\n")
    .find((entry) => entry.startsWith(`${name}=`))
  if (!line) {
    throw new Error(`the environment recorded no ${name}; run make e2e-up first`)
  }
  return line.slice(name.length + 1)
}

/**
 * A file in the environment's own directory, which the workflow does not
 * upload: an enrollment file holds a bootstrap secret.
 */
export function outPath(name: string): string {
  return path.join(outDir, name)
}

/**
 * Changes something in the cluster as its administrator, the way another team
 * or a failure would, and returns what the action printed.
 */
export async function act(...args: string[]): Promise<string> {
  const { stdout } = await run(path.join(root, "hack/e2e/act.sh"), args, {
    env: { ...process.env, OUT_DIR: outDir },
    timeout: 15 * 60_000,
  })
  return stdout.trim()
}

/**
 * What a password field holds instead of the password. A trace keeps the
 * arguments of every call, the value of every input in its DOM snapshots and
 * the body of every request, and the HTML report keeps each step's
 * arguments, so a password the page held would be in all of them.
 */
const passwordPlaceholder = "placeholder: the test runner sends the password"

/** A sign-in form whose password the page must not hold. */
export interface PasswordForm {
  field: Locator
  submit: Locator
  password: string
  /** The request the form sends, as page.route matches it. */
  url: string | RegExp | ((url: URL) => boolean)
  /** The certificate the server's must chain to, a path. */
  ca: string
  /** Where the server's host name resolves, when a resolver rule maps it for the browser. */
  address?: string
}

/**
 * Sends a sign-in form without the page ever holding the password. The field
 * gets a placeholder; the request the form sends goes out again from this
 * process with the password in its place, and the browser gets the server's
 * answer as its own, cookies and redirects included. hack/e2e/secretscan
 * refuses an artifact that holds a password all the same.
 */
export async function sendPassword(page: Page, form: PasswordForm): Promise<void> {
  let taken = false
  let answered: () => void = () => undefined
  let failed: (error: unknown) => void = () => undefined
  const answer = new Promise<void>((resolve, reject) => {
    answered = resolve
    failed = reject
  })
  // Settled after the wait gave up, it is not an unhandled rejection.
  answer.catch(() => undefined)
  const handler = async (route: Route) => {
    // A page that loads the form, or follows the answer's redirect, goes on
    // as it would.
    if (taken || route.request().method() !== "POST") {
      await route.fallback()
      return
    }
    taken = true
    try {
      await route.fulfill(await resend(route.request(), form))
      answered()
    } catch (error) {
      await route.abort().catch(() => undefined)
      failed(error)
    }
  }
  await page.route(form.url, handler)
  try {
    await form.field.fill(passwordPlaceholder)
    await form.submit.click()
    await within(answer, 60_000, "the form sent no request with its password")
  } finally {
    await page.unroute(form.url, handler)
  }
}

/** Request and response headers that belong to one connection, not to the exchange. */
const perConnection = new Set([
  "connection",
  "content-length",
  "host",
  "keep-alive",
  "proxy-connection",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
  // Sent without it, the answer arrives as the page reads it.
  "accept-encoding",
])

/** Sends the form's request from this process, with the password in it. */
async function resend(
  request: Request,
  form: PasswordForm
): Promise<{ status: number; headers: Record<string, string>; body: Buffer }> {
  const target = new URL(request.url())
  const headers: Record<string, string> = {}
  for (const [name, value] of Object.entries(await request.allHeaders())) {
    if (!name.startsWith(":") && !perConnection.has(name)) {
      headers[name] = value
    }
  }
  const body = withPassword(headers["content-type"] ?? "", request.postData(), form.password)
  headers.host = target.host
  headers["content-length"] = String(Buffer.byteLength(body))
  const ca = readFileSync(form.ca)
  return new Promise((resolve, reject) => {
    const outgoing = httpsRequest(
      {
        hostname: form.address ?? target.hostname,
        port: target.port || 443,
        path: target.pathname + target.search,
        method: "POST",
        headers,
        servername: target.hostname,
        ca,
        agent: false,
      },
      (incoming) => {
        const chunks: Buffer[] = []
        incoming.on("data", (chunk: Buffer) => chunks.push(chunk))
        incoming.on("error", reject)
        incoming.on("end", () => {
          const answer: Record<string, string> = {}
          for (const [name, value] of Object.entries(incoming.headers)) {
            if (value !== undefined && !perConnection.has(name)) {
              // Playwright takes several Set-Cookie headers one to a line.
              answer[name] = Array.isArray(value) ? value.join("\n") : value
            }
          }
          const encoding = answer["content-encoding"]
          if (encoding !== undefined && encoding !== "identity") {
            reject(new Error(`${target.origin} answered in ${encoding}, which it was not offered`))
            return
          }
          resolve({
            status: incoming.statusCode ?? 0,
            headers: answer,
            body: Buffer.concat(chunks),
          })
        })
      }
    )
    outgoing.on("error", reject)
    outgoing.end(body)
  })
}

/**
 * The form's body with the password in place of the placeholder. Neither the
 * body nor the password goes into an error.
 */
function withPassword(contentType: string, body: string | null, password: string): string {
  const type = contentType.split(";")[0].trim().toLowerCase()
  if (type === "application/json" && body !== null) {
    const fields = JSON.parse(body) as Record<string, unknown>
    if (fields.password === passwordPlaceholder) {
      return JSON.stringify({ ...fields, password })
    }
  } else if (type === "application/x-www-form-urlencoded" && body !== null) {
    const fields = new URLSearchParams(body)
    if (fields.get("password") === passwordPlaceholder) {
      fields.set("password", password)
      return fields.toString()
    }
  } else {
    throw new Error(`the form sent ${type || "no content type"}, which holds no password field`)
  }
  throw new Error("the form's password field did not hold the placeholder")
}

/** Waits for a promise, and fails with a message once the time is up. */
async function within<T>(promise: Promise<T>, ms: number, message: string): Promise<T> {
  let timer: NodeJS.Timeout | undefined
  const late = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(message)), ms)
  })
  try {
    return await Promise.race([promise, late])
  } finally {
    clearTimeout(timer)
  }
}

/**
 * Starts `ptah-console local` as one person, for some namespaces, and
 * returns the link it printed.
 */
async function start(
  who: Persona,
  namespaces: string[]
): Promise<{ link: string; child: ChildProcess }> {
  const audit = openSync(path.join(outDir, `${who}-audit.log`), "a")
  const child = spawn(
    binary,
    [
      "local",
      "--kubeconfig",
      path.join(outDir, `${who}.kubeconfig`),
      ...namespaces.flatMap((namespace) => ["--namespace", namespace]),
      "--no-open",
    ],
    { stdio: ["ignore", "pipe", audit] }
  )
  closeSync(audit)
  const link = await new Promise<string>((resolve, reject) => {
    let printed = ""
    const timer = setTimeout(() => {
      reject(new Error(`ptah-console as ${who} printed no link: ${printed}`))
    }, 60_000)
    child.stdout?.on("data", (chunk: Buffer) => {
      printed += chunk.toString()
      const found = /^\s+(http:\/\/\S+\/#t=[A-Za-z0-9_-]+)$/m.exec(printed)
      if (found) {
        clearTimeout(timer)
        resolve(found[1])
      }
    })
    child.on("exit", (code) => {
      clearTimeout(timer)
      reject(new Error(`ptah-console as ${who} exited with ${code}: ${printed}`))
    })
  })
  return { link, child }
}

/**
 * Shows a caption near the top of the page for the recording. It is built
 * with the DOM and CSSOM, which the page's Content Security Policy allows.
 */
async function showCaption(page: Page, text: string, at: "top" | "bottom" = "top") {
  await page.evaluate(
    ([line, edge]) => {
      let box = document.getElementById("e2e-caption")
      if (!box) {
        box = document.createElement("div")
        box.id = "e2e-caption"
        Object.assign(box.style, {
          position: "fixed",
          left: "50%",
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
      // Near the top by default; at the bottom when what the scene is about
      // sits at the top of the page.
      box.style.top = edge === "top" ? "74px" : ""
      box.style.bottom = edge === "bottom" ? "24px" : ""
      box.textContent = line
      box.style.display = line ? "block" : "none"
    },
    [text, at] as const
  )
}

export interface Lab {
  /**
   * Starts a console as this person, for the lab namespace unless others are
   * named, and opens the link it printed.
   */
  signIn(who: Persona, namespaces?: string[]): Promise<void>
  /**
   * Opens the hosted console and signs in with an account's login and
   * password, the current one unless another is given.
   */
  signInHosted(account: Account, password?: string): Promise<void>
  /**
   * Fills in the hosted console's sign-in form on the page and sends it,
   * with the account's current password unless another is given. The page
   * never holds the password: see sendPassword.
   */
  sendHostedSignIn(account: Account, password?: string): Promise<void>
  /** The origin of the console the page is signed in to. */
  origin(): string
  /** Shows a caption, holds it long enough to read, and takes it away. */
  caption(text: string, holdMs?: number, at?: "top" | "bottom"): Promise<void>
  /**
   * Checks the page as it stands against WCAG 2.1 A and AA with axe. A
   * finding fails the row at its end, so the rest of the row still runs.
   */
  audit(screen: string): Promise<void>
  /**
   * Waits for something slow, such as the operator applying a plan, and
   * marks the wait so the recording can leave it out.
   */
  skip<T>(wait: () => Promise<T>): Promise<T>
}

/** What a row's recording needs to be cut and put in order. */
export interface Film {
  title: string
  /** The row file, so a film can be made of some of them. */
  file: string
  startedAt: number
  /**
   * Stretches to leave out, in seconds from the start of the recording, and
   * how much of each edge to keep so a cut reads as a cut.
   */
  cuts: { from: number; to: number; keep: number }[]
}

export const test = base.extend<{ lab: Lab }>({
  lab: async ({ page }, provide, testInfo) => {
    // The video starts with the page, which exists by now.
    const startedAt = Date.now()
    const seconds = () => (Date.now() - startedAt) / 1000
    const children: ChildProcess[] = []
    const cuts: Film["cuts"] = []
    let origin = ""
    const sendHostedSignIn = async (account: Account, password?: string) => {
      const form = page.getByRole("form", { name: "Sign in" })
      await form.getByLabel("Login").fill(`${account}@e2e.test`)
      await sendPassword(page, {
        field: form.getByLabel("Password"),
        submit: form.getByRole("button", { name: "Sign in" }),
        password: password ?? fact(`HOSTED_${account.toUpperCase()}_PASSWORD`),
        url: `${fact("HOSTED_URL")}/api/ptah.console.v1.SessionService/Login`,
        ca: outPath("hosted-tls.crt"),
      })
    }
    await provide({
      async signIn(who, namespaces = [fact("NAMESPACE")]) {
        // Starting a console shows a blank page until it is signed in; the
        // film leaves that out whole. Before a row's first sign-in the page
        // has been blank since the recording started, while the row set the
        // cluster up.
        const from = children.length === 0 && cuts.length === 0 ? 0 : seconds()
        // The session cookie is per host, so one browser holds one person.
        for (const child of children.splice(0)) {
          child.kill()
        }
        const started = await start(who, namespaces)
        children.push(started.child)
        origin = new URL(started.link).origin
        await page.goto(started.link)
        // Signed in once the page names the identity the session acts as.
        await expect(page.getByText(`e2e-${who}`).first()).toBeVisible({ timeout: 60_000 })
        cuts.push({ from, to: seconds(), keep: 0 })
      },
      async signInHosted(account, password) {
        const from = children.length === 0 && cuts.length === 0 ? 0 : seconds()
        for (const child of children.splice(0)) {
          child.kill()
        }
        origin = fact("HOSTED_URL")
        // A row may sign in as one account and then another: the earlier
        // session would otherwise send the sign-in page on to the console.
        await page.context().clearCookies()
        await page.goto(`${origin}/login`)
        const form = page.getByRole("form", { name: "Sign in" })
        await expect(form).toBeVisible({ timeout: 60_000 })
        cuts.push({ from, to: seconds(), keep: 0 })
        await sendHostedSignIn(account, password)
        await expect(page.getByText(fact(`HOSTED_${account.toUpperCase()}`)).first()).toBeVisible({
          timeout: 60_000,
        })
      },
      sendHostedSignIn,
      origin: () => origin,
      async caption(text, holdMs = 3500, at = "top") {
        await showCaption(page, text, at)
        await page.waitForTimeout(holdMs)
        // A caption says what the page shows now; the next action changes
        // that, so it does not stay over it.
        await showCaption(page, "")
      },
      async audit(screen) {
        // A page or dialog fades in; measured halfway, its text blends with
        // what is behind it. Wait for every animation and transition that
        // ends, and leave the ones that never do, such as a spinner.
        await page.evaluate(async () => {
          const ending = document
            .getAnimations()
            .filter((animation) => animation.effect?.getComputedTiming().endTime !== Infinity)
          await Promise.all(ending.map((animation) => animation.finished.catch(() => undefined)))
        })
        const results = await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
          .exclude("#e2e-caption")
          .analyze()
        const findings = results.violations.flatMap((violation) =>
          violation.nodes.map(
            (node) =>
              `${violation.id}: ${node.target.join(" ")}: ${node.any.map((check) => check.message).join("; ")}`
          )
        )
        expect.soft(findings, `axe on ${screen}`).toEqual([])
      },
      async skip(wait) {
        // A caption belongs to what it was shown over, not to what comes
        // after the cut.
        await showCaption(page, "")
        const from = seconds()
        try {
          return await wait()
        } finally {
          cuts.push({ from, to: seconds(), keep: 1.5 })
        }
      },
    })
    for (const child of children) {
      child.kill()
    }
    const film: Film = {
      title: testInfo.title,
      file: path.basename(testInfo.file),
      startedAt,
      cuts,
    }
    writeFileSync(testInfo.outputPath("film.json"), JSON.stringify(film))
  },
})

export { expect }
