import { Button, Card } from "@ptah/console-ui"
import type { ErrorComponentProps } from "@tanstack/react-router"

/** A page that failed to render says so, and offers to load it again. */
export function RouteError({ error, reset }: ErrorComponentProps) {
  return (
    <Card title="This page failed">
      <span role="alert" className="text-sm text-red">
        {error instanceof Error ? error.message : String(error)}
      </span>
      <Button className="self-start" onClick={reset}>
        Try again
      </Button>
    </Card>
  )
}
