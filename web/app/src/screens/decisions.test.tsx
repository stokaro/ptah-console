import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { screen } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { DecisionKind, DecisionSchema, DecisionState } from "../gen/ptah/console/v1/decisions_pb"
import { decisionsCsv } from "../model/decision"
import { renderConsole } from "../test/console"

const approval = create(DecisionSchema, {
  kind: DecisionKind.SCHEMA_APPROVAL,
  ref: { namespace: "application", name: "ptah-console-0123", uid: "uid-approval" },
  ownerName: "storefront",
  planName: "storefront-p-8f2c",
  planFingerprint: "sha256:9c2400000000000000007b10",
  decidedBy: '=HYPERLINK("https://example")',
  decidedAt: timestampFromDate(new Date("2026-10-19T10:00:00Z")),
  state: DecisionState.CONSUMED,
})

describe("Auditor", () => {
  it("lists who decided what, as admission recorded it", async () => {
    const { container } = renderConsole("/decisions", { decisions: [approval] })

    expect(
      await screen.findByRole("link", { name: "Approved storefront-p-8f2c for storefront" })
    ).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "consumed" })).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("exports CSV that a spreadsheet does not run", () => {
    const csv = decisionsCsv([approval])

    expect(csv.split("\r\n")[1]).toBe(
      `2026-10-19T10:00:00.000Z,schema-approval,application,ptah-console-0123,storefront,storefront-p-8f2c,sha256:9c2400000000000000007b10,,"'=HYPERLINK(""https://example"")",consumed`
    )
  })
})
