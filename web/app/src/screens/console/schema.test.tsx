import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it, vi } from "vitest"

import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"

import { TargetSchema } from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, waitingSchema } from "../../test/console"

const spec = `desired:
  ociRef: oci://registry.example/schemas/demo@sha256:5f2ce7ae
policy:
  apply: OnApproval
target:
  engine: PostgreSQL
  urlFrom:
    key: url
    name: storefront-database
`

const schema = waitingSchema((s) => {
  s.specYaml = spec
  s.target = create(TargetSchema, {
    engine: "PostgreSQL",
    secretName: "storefront-database",
    secretKey: "url",
    lastObservedAt: minutesAgo(5),
    highestDriftSeverity: "Destructive",
    driftFindingCount: 3,
    driftFindings: [
      { category: "Column", count: 2, severity: "Destructive" },
      { category: "Grant", count: 1, severity: "Privilege" },
    ],
  })
  s.source!.verified = true
  s.source!.verifiedAt = minutesAgo(13)
  s.nextReconciliation = timestampFromDate(new Date(Date.now() + 10 * 60_000))
})

describe("Schema detail", () => {
  it("shows where the schema stands, its plan, its conditions, artifact and target", async () => {
    const { container } = renderConsole("/console/schemas/application/storefront", {
      schemas: [schema],
    })

    expect(
      await screen.findByRole("heading", { name: /PtahSchema \/ storefront/ })
    ).toBeInTheDocument()
    const plan = screen.getByRole("region", { name: "Current plan" })
    expect(within(plan).getByText("storefront-p-8f2c")).toBeInTheDocument()
    expect(within(plan).getByText("Destructive")).toBeInTheDocument()
    expect(within(plan).getByText("Not approved")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Review plan" })).toHaveAttribute(
      "href",
      "/console/schemas/application/storefront/plan?plan=storefront-p-8f2c"
    )
    const conditions = screen.getByRole("table", { name: "Conditions" })
    expect(within(conditions).getByRole("row", { name: /ApprovalRequired/ })).toHaveTextContent(
      "AwaitingApproval"
    )
    const target = screen.getByRole("region", { name: "Target" })
    expect(target).toHaveTextContent("Secret storefront-database, key url")
    expect(within(target).getByRole("row", { name: /Column/ })).toHaveTextContent("2")
    expect(screen.getByRole("region", { name: "Artifact" })).toHaveTextContent("Yes, 13 min ago")
    expect(screen.getByRole("region", { name: "Reconciliation" })).toHaveTextContent(
      /Next read\s*in 9 min|Next read\s*in 10 min/
    )
    expect(await axe(container)).toHaveNoViolations()
  })

  it("shows the spec read-only and copies it as a patch the person runs", async () => {
    const user = userEvent.setup()
    const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue()
    renderConsole("/console/schemas/application/storefront", { schemas: [schema] })

    const shown = await screen.findByLabelText("Spec of storefront")
    expect(shown).toHaveTextContent("name: storefront-database")
    expect(screen.queryByRole("textbox")).toBeNull()
    await user.click(screen.getByRole("button", { name: "Copy as patch" }))

    expect(write).toHaveBeenCalledWith(
      `kubectl -n application patch ptahschema storefront --type merge --patch-file /dev/stdin <<'YAML'
spec:
  desired:
    ociRef: oci://registry.example/schemas/demo@sha256:5f2ce7ae
  policy:
    apply: OnApproval
  target:
    engine: PostgreSQL
    urlFrom:
      key: url
      name: storefront-database
YAML`
    )
    expect(screen.getAllByRole("status").some((s) => s.textContent === "Copied.")).toBe(true)
  })

  it("opens from the resource list", async () => {
    const user = userEvent.setup()
    renderConsole("/console/resources", { schemas: [schema] })

    await user.click(await screen.findByRole("link", { name: /storefront/ }))

    expect(
      await screen.findByRole("heading", { name: /PtahSchema \/ storefront/ })
    ).toBeInTheDocument()
  })

  it("offers no suspend to a read-only console", async () => {
    const user = userEvent.setup()
    renderConsole("/console/schemas/application/storefront", { schemas: [schema], readOnly: true })

    await user.click(await screen.findByRole("button", { name: "Actions" }))

    const menu = await screen.findByRole("menu")
    expect(within(menu).getByRole("menuitem", { name: "Copy kubectl command" })).toBeVisible()
    expect(within(menu).queryByRole("menuitem", { name: "Suspend…" })).toBeNull()
  })

  it("suspends from the Actions menu, and copies the kubectl command that shows the same state", async () => {
    const user = userEvent.setup()
    const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue()
    const { container } = renderConsole("/console/schemas/application/storefront", {
      schemas: [schema],
    })

    await user.click(await screen.findByRole("button", { name: "Actions" }))
    expect(await axe(container)).toHaveNoViolations()
    await user.click(screen.getByRole("menuitem", { name: "Copy kubectl command" }))

    expect(write).toHaveBeenCalledWith("kubectl ptah schema storefront -n application")
    await user.click(screen.getByRole("button", { name: "Actions" }))
    await user.click(await screen.findByRole("menuitem", { name: "Suspend…" }))
    expect(await screen.findByRole("alertdialog")).toHaveTextContent("Suspend storefront?")
  })

  it("says so when there is no such schema", async () => {
    renderConsole("/console/schemas/application/missing", { schemas: [schema] })

    expect(
      await screen.findByText("No schema missing in application that this identity may read.")
    ).toBeInTheDocument()
  })
})
