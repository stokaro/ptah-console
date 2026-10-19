import { Wordmark } from "@ptah/console-ui"

import { BuildFooter } from "./BuildFooter"

export function App() {
  return (
    <div className="flex min-h-screen flex-col">
      <header className="flex items-center gap-3 border-b border-slate-200 px-5 py-3">
        <Wordmark />
      </header>
      <main className="mx-auto w-full max-w-3xl flex-1 px-5 py-8">
        <h1 className="text-2xl font-semibold tracking-tight">Ptah Console</h1>
        <p className="mt-2 text-slate-600">
          The console is under construction. It will show what Ptah Operator is doing with your
          schemas and migrations, and let you approve the exact plan you reviewed.
        </p>
      </main>
      <BuildFooter />
    </div>
  )
}
