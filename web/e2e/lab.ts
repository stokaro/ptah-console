import AxeBuilder from "@axe-core/playwright"
import { test as base, expect, type Page } from "@playwright/test"
import { execFile, spawn, type ChildProcess } from "node:child_process"
import { closeSync, openSync, readFileSync, writeFileSync } from "node:fs"
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
  /** The origin of the console the page is signed in to. */
  origin(): string
  /** Shows a caption and holds it long enough to read. */
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
        await page.goto(`${origin}/login`)
        const form = page.getByRole("form", { name: "Sign in" })
        await expect(form).toBeVisible({ timeout: 60_000 })
        cuts.push({ from, to: seconds(), keep: 0 })
        await form.getByLabel("Login").fill(`${account}@e2e.test`)
        await form
          .getByLabel("Password")
          .fill(password ?? fact(`HOSTED_${account.toUpperCase()}_PASSWORD`))
        await form.getByRole("button", { name: "Sign in" }).click()
        await expect(page.getByText(fact(`HOSTED_${account.toUpperCase()}`)).first()).toBeVisible({
          timeout: 60_000,
        })
      },
      origin: () => origin,
      async caption(text, holdMs = 3500, at = "top") {
        await showCaption(page, text, at)
        await page.waitForTimeout(holdMs)
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
    const film: Film = { title: testInfo.title, startedAt, cuts }
    writeFileSync(testInfo.outputPath("film.json"), JSON.stringify(film))
  },
})

export { expect }
