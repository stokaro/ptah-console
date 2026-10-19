import { Code, ConnectError } from "@connectrpc/connect"
import { Button, ConfirmDialog, DialogDescription } from "@ptah/console-ui"
import { useId, useState, type ReactNode } from "react"

import {
  DecisionOutcome,
  type Decision,
  type DecisionResult,
} from "../gen/ptah/console/v1/decisions_pb"

type Step =
  | { kind: "confirm"; reviewed: boolean }
  | { kind: "sending" }
  | { kind: "result"; result: DecisionResult }
  | { kind: "checking"; result: DecisionResult }
  | { kind: "failed"; message: string }

export interface DecisionDialogProps {
  onClose: () => void
  where: string
  as: string
  /** The question, such as "Approve storefront-p-8f2c?" */
  title: string
  /** What sending does, in a sentence or two. */
  explanation: ReactNode
  /** Asks the person to confirm they reviewed what changes. */
  needsReview?: boolean
  /** False while what the request needs is still being read. */
  ready: boolean
  send: () => Promise<DecisionResult>
  /** Reads the decision back by name after an unknown outcome. */
  readBack?: (name: string) => Promise<Decision | undefined>
  admittedTitle: string
  admittedText: (decision?: Decision) => string
  /** Shown beside Close once the decision is admitted. */
  next?: ReactNode
  /** Shown beside Close when what was reviewed changed. */
  reviewAgain?: ReactNode
}

/**
 * Sends one decision and says how it ended: admitted, changed, forbidden,
 * refused or unknown. The server's own words are shown as they came.
 */
export function DecisionDialog(props: DecisionDialogProps) {
  const [step, setStep] = useState<Step>({ kind: "confirm", reviewed: !props.needsReview })
  const reviewId = useId()

  async function send() {
    setStep({ kind: "sending" })
    try {
      setStep({ kind: "result", result: await props.send() })
    } catch (error) {
      setStep({ kind: "failed", message: failureText(error) })
    }
  }

  async function check(result: DecisionResult) {
    setStep({ kind: "checking", result })
    try {
      const decision = await props.readBack?.(result.name)
      setStep({
        kind: "result",
        result: decision
          ? { ...result, outcome: DecisionOutcome.ADMITTED, decision, message: "" }
          : { ...result, outcome: DecisionOutcome.UNSPECIFIED, message: "" },
      })
    } catch (error) {
      setStep({ kind: "failed", message: failureText(error) })
    }
  }

  const close = <Button onClick={props.onClose}>Close</Button>
  const busy = step.kind === "sending" || step.kind === "checking"
  const view = describe(step, props)

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => (open ? undefined : props.onClose())}
      title={view.title}
      titleTone={view.tone}
      where={props.where}
      as={props.as}
      busy={busy}
    >
      {step.kind === "confirm" ? (
        <>
          <DialogDescription className="m-0 text-[13.5px] leading-normal text-fg2">
            {props.explanation}
          </DialogDescription>
          {props.needsReview ? (
            <label
              htmlFor={reviewId}
              className="grid min-h-12 cursor-pointer grid-cols-[24px_minmax(0,1fr)] items-center gap-2.5 rounded-xl border border-line px-3 py-2.5 text-sm"
            >
              <input
                id={reviewId}
                type="checkbox"
                checked={step.reviewed}
                onChange={(event) => setStep({ kind: "confirm", reviewed: event.target.checked })}
                className="size-[22px] accent-[var(--pri)]"
              />
              I reviewed what this changes.
            </label>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <Button onClick={props.onClose}>Cancel</Button>
            <Button
              variant="primary"
              disabled={!step.reviewed || !props.ready}
              onClick={() => void send()}
            >
              Confirm
            </Button>
          </div>
        </>
      ) : null}
      {busy ? (
        <p role="status" className="m-0 flex items-center gap-3 p-4 text-sm">
          <span className="box-border size-5 animate-[pt-spin_.8s_linear_infinite] rounded-full border-2 border-pri border-t-transparent" />
          {step.kind === "sending" ? "Submitting…" : "Reading it back…"}
        </p>
      ) : null}
      {step.kind === "result" || step.kind === "failed" ? (
        <>
          <DialogDescription className="m-0 text-[13.5px] leading-normal">
            {view.text}
          </DialogDescription>
          {view.serverWords ? (
            <pre className="m-0 rounded-[10px] bg-greybg p-3 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">
              {view.serverWords}
            </pre>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            {close}
            {step.kind === "result" && step.result.outcome === DecisionOutcome.ADMITTED
              ? props.next
              : null}
            {step.kind === "result" && step.result.outcome === DecisionOutcome.CHANGED
              ? props.reviewAgain
              : null}
            {step.kind === "result" &&
            step.result.outcome === DecisionOutcome.UNKNOWN &&
            props.readBack ? (
              <Button variant="primary" onClick={() => void check(step.result)}>
                Check result
              </Button>
            ) : null}
          </div>
        </>
      ) : null}
    </ConfirmDialog>
  )
}

interface View {
  title: string
  tone: "fg" | "green" | "amber" | "red" | "violet"
  text?: string
  serverWords?: string
}

function describe(step: Step, props: DecisionDialogProps): View {
  if (step.kind === "failed") {
    return { title: "The console could not send it", tone: "red", text: step.message }
  }
  if (step.kind !== "result") {
    return { title: props.title, tone: "fg" }
  }
  const { result } = step
  switch (result.outcome) {
    case DecisionOutcome.ADMITTED:
      return {
        title: props.admittedTitle,
        tone: "green",
        text: props.admittedText(result.decision),
      }
    case DecisionOutcome.CHANGED:
      return { title: "It changed", tone: "amber", text: result.message }
    case DecisionOutcome.FORBIDDEN:
      return {
        title: "Kubernetes does not let you",
        tone: "red",
        text: "RBAC refused this identity. Nothing was stored.",
        serverWords: result.message,
      }
    case DecisionOutcome.REFUSED:
      return {
        title: "Refused",
        tone: "red",
        text: "The operator's admission or the API server refused it. Nothing was stored.",
        serverWords: result.message,
      }
    case DecisionOutcome.UNKNOWN:
      return {
        title: "Submission outcome unknown",
        tone: "violet",
        text: "The response was lost. Kubernetes may already have it. Check before doing anything else.",
        serverWords: result.message,
      }
    default:
      return {
        title: "Not stored",
        tone: "amber",
        text: "Kubernetes has no decision under that name. It was not stored; you can send it again.",
      }
  }
}

function failureText(error: unknown) {
  const failure = ConnectError.from(error)
  if (failure.code === Code.PermissionDenied) {
    return failure.rawMessage
  }
  if (failure.code === Code.Unauthenticated) {
    return "The session ended. Open a new link from ptah-console in your terminal."
  }
  return `The console did not answer (${failure.rawMessage}). Look at the resource again before sending it once more.`
}
