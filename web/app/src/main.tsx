import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import { App } from "./App"
import { AppProviders } from "./AppProviders"
import "./index.css"

const root = document.getElementById("root")
if (!root) {
  throw new Error("index.html has no #root element")
}

createRoot(root).render(
  <StrictMode>
    <AppProviders>
      <App />
    </AppProviders>
  </StrictMode>
)
