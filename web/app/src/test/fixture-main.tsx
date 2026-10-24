// The console against the test fixtures, for looking at the screens without
// a cluster: `npm run dev --workspace app`, then open /fixture.html. The
// production build reads index.html only, so none of this ships.
import { create } from "@bufbuild/protobuf"
import { createBrowserHistory } from "@tanstack/react-router"
import { QueryClient } from "@tanstack/react-query"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import { App } from "../App"
import { AppProviders } from "../AppProviders"
import { DecisionKind, DecisionOutcome, DecisionSchema } from "../gen/ptah/console/v1/decisions_pb"
import { applyAccent, readAccent } from "../accent"
import { applyTheme, readTheme } from "../theme"
import { consoleTransport, result, waitingSchema } from "./console"
import "../index.css"

applyTheme(readTheme())
applyAccent(readAccent())
// The router knows the console's paths, not this file's.
window.history.replaceState(null, "", "/approve")

const transport = consoleTransport({
  username: "system:serviceaccount:application:demo-approver",
  schemas: [waitingSchema()],
  approve: async (request) => {
    await new Promise((resolve) => setTimeout(resolve, 600))
    return result({
      outcome: DecisionOutcome.ADMITTED,
      decision: create(DecisionSchema, {
        kind: DecisionKind.SCHEMA_APPROVAL,
        planName: request.planName,
        decidedBy: "system:serviceaccount:application:demo-approver",
      }),
    })
  },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppProviders transport={transport} queryClient={new QueryClient()}>
      <App history={createBrowserHistory()} />
    </AppProviders>
  </StrictMode>
)
