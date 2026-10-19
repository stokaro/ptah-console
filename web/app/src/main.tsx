import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import { createApiTransport } from "./api/transport"
import { App } from "./App"
import { AppProviders } from "./AppProviders"
import { exchangeLaunchToken } from "./launch"
import "./index.css"

const root = document.getElementById("root")
if (!root) {
  throw new Error("index.html has no #root element")
}

const transport = createApiTransport()

function render(launchError?: string) {
  createRoot(root!).render(
    <StrictMode>
      <AppProviders transport={transport}>
        <App launchError={launchError} />
      </AppProviders>
    </StrictMode>
  )
}

exchangeLaunchToken(transport, window.location, window.history).then(
  () => render(),
  (error: unknown) => render(error instanceof Error ? error.message : String(error))
)
