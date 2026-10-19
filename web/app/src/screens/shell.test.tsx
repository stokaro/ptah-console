import { Code, ConnectError } from "@connectrpc/connect"
import type { MessageInitShape } from "@bufbuild/protobuf"
import { render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Answer } from "../gen/ptah/console/v1/resources_pb"
import { WatchedKind, type SubscribeResponseSchema } from "../gen/ptah/console/v1/watch_pb"
import { heartbeat, renderConsole, waitingSchema } from "../test/console"
import { RouteError } from "./RouteError"

type Event = MessageInitShape<typeof SubscribeResponseSchema>

/** A watch the test drives: each push is the next event; fail ends it. */
function scripted() {
  const queue: (Event | Error)[] = []
  let wake: () => void = () => {}
  return {
    push(item: Event | Error) {
      queue.push(item)
      wake()
    },
    async *stream(signal: AbortSignal) {
      while (!signal.aborted) {
        const next = queue.shift()
        if (next instanceof Error) {
          throw next
        }
        if (next) {
          yield next
          continue
        }
        await new Promise<void>((resolve) => {
          wake = resolve
          signal.addEventListener("abort", () => resolve())
        })
      }
    },
  }
}

const lost = (reconnecting: boolean): Event => ({
  event: {
    case: "lost",
    value: {
      kind: WatchedKind.SCHEMA,
      namespace: "application",
      reason: "watch stopped",
      reconnecting,
    },
  },
})

describe("The shell", () => {
  it("holds decisions back while the cluster is out of reach, and gives them back after", async () => {
    const watch = scripted()
    renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      watch: watch.stream,
    })
    watch.push(heartbeat())
    expect(await screen.findByRole("button", { name: "Approve this plan…" })).toBeInTheDocument()
    expect(screen.getByText(/Live · current/)).toBeInTheDocument()

    watch.push(lost(true))

    expect(await screen.findByText(/Live updates stopped at/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Approve this plan…" })).toBeNull()
    expect(screen.getByText(/Decisions wait until it is back/)).toBeInTheDocument()

    watch.push(heartbeat())

    await waitFor(() => expect(screen.queryByText(/Live updates stopped at/)).toBeNull())
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeInTheDocument()
  })

  it("does not hold decisions back when the identity may not watch a kind", async () => {
    const watch = scripted()
    renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      watch: watch.stream,
    })
    watch.push(heartbeat())
    watch.push(lost(false))

    expect(await screen.findByRole("button", { name: "Approve this plan…" })).toBeInTheDocument()
    expect(screen.queryByText(/Live updates stopped at/)).toBeNull()
  })

  it("says the page is not live when the identity may not watch at all", async () => {
    const watch = scripted()
    renderConsole("/approve", { schemas: [waitingSchema()], watch: watch.stream })
    watch.push(new ConnectError("may not watch", Code.PermissionDenied))

    expect(await screen.findByText(/Not live: this identity may not watch/)).toBeInTheDocument()
    expect(screen.queryByText(/Live updates stopped at/)).toBeNull()
    expect(screen.getByRole("button", { name: "Approve…" })).toBeInTheDocument()
  })

  it("says when the cluster does not serve the operator's API", async () => {
    renderConsole("/releases", {
      operatorApi: {
        served: false,
        problem:
          "This cluster does not serve operator.ptah.run/v1alpha1: Ptah Operator is not installed here, or its CRDs are not.",
      },
    })

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "does not serve operator.ptah.run/v1alpha1"
    )
  })

  it("names the resources the cluster does not serve", async () => {
    renderConsole("/releases", {
      operatorApi: {
        served: true,
        missing: ["ptahmigrationrunacknowledgments"],
        problem: "Older CRDs.",
      },
    })

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Not served: ptahmigrationrunacknowledgments"
    )
  })

  it("says when a namespace holds more than one read returns", async () => {
    renderConsole("/releases", { schemas: [waitingSchema()], truncated: ["application"] })

    expect(await screen.findByText(/only the first are shown/)).toHaveTextContent("application")
  })

  it.each([
    { name: "an approver starts at the approver view", capabilities: {}, path: "/approve" },
    {
      name: "an on-call reader starts at incidents",
      capabilities: {
        approveSchemaPlans: Answer.DENIED,
        approveMigrationPlans: Answer.DENIED,
        acknowledgeRuns: Answer.ALLOWED,
      },
      path: "/incidents",
    },
    {
      name: "anyone else starts at releases",
      capabilities: {
        approveSchemaPlans: Answer.DENIED,
        approveMigrationPlans: Answer.DENIED,
        acknowledgeRuns: Answer.DENIED,
        readLogs: Answer.DENIED,
        listSchemas: Answer.ALLOWED,
      },
      path: "/releases",
    },
  ])("$name", async ({ capabilities, path }) => {
    const { history } = renderConsole("/", {
      capabilities: Object.keys(capabilities).length === 0 ? undefined : capabilities,
    })

    await waitFor(() => expect(history.location.pathname).toBe(path))
  })

  it("lets a failed page be tried again", () => {
    render(
      <RouteError
        error={new Error("could not render")}
        reset={() => {}}
        info={{ componentStack: "" }}
      />
    )

    expect(screen.getByRole("alert")).toHaveTextContent("could not render")
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
  })
})
