import { create } from "@bufbuild/protobuf"
import { Code, ConnectError } from "@connectrpc/connect"
import { useMutation } from "@connectrpc/connect-query"
import { Button, ConfirmDialog, DialogDescription, StatusGlyph, Terminal } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { useId, useState } from "react"

import { useSession } from "../../api/resources"
import {
  DecisionOutcome,
  DecisionService,
  ResourceKind,
  SetSourceResponseSchema,
  type SetSourceResponse,
} from "../../gen/ptah/console/v1/decisions_pb"
import { MigrationService, SchemaService } from "../../gen/ptah/console/v1/resources_pb"
import type { ResourceView } from "../../model/resource"
import { sourceChange, type SourceChange } from "../../model/sourcechange"
import { parsePinned, pinnedForm } from "../../model/sourceref"
import { where } from "../common"

type Step =
  | { kind: "edit" }
  | { kind: "checking" }
  | { kind: "sending" }
  | { kind: "result"; response: SetSourceResponse }
  | { kind: "reading"; response: SetSourceResponse }
  | { kind: "read"; stored: boolean; now: string }
  | { kind: "failed"; message: string }

export interface ChangeSourceDialogProps {
  resource: ResourceView
  context?: string
  onClose: () => void
}

/**
 * Points an OCI-sourced schema or migration at another artifact, given as a
 * reference pinned by digest, the way kubectl ptah set-source does. The
 * reference is checked inline by the operator's rules, then by the API
 * server as a dry run that stores nothing, and only the reference that check
 * accepted can be sent. The change replaces the reference and nothing else.
 */
export function ChangeSourceDialog({ resource, context, onClose }: ChangeSourceDialogProps) {
  const session = useSession()
  const queryClient = useQueryClient()
  const setSource = useMutation(DecisionService.method.setSource)
  const getSchema = useMutation(SchemaService.method.getSchema)
  const getMigration = useMutation(MigrationService.method.getMigration)
  // The object and the reference the change is for are held from the moment
  // the dialog opens: the page goes on following the name behind it, and a
  // check and the change it allows both go to the object the person saw.
  const [change] = useState(() => sourceChange(resource))
  const [value, setValue] = useState("")
  const [checked, setChecked] = useState<{ value: string; response: SetSourceResponse }>()
  const [step, setStep] = useState<Step>({ kind: "edit" })
  const inputId = useId()
  const hintId = useId()
  const problemId = useId()
  const gateId = useId()

  if (!change) {
    return null
  }
  // What can be sent now follows the resource as the page reads it.
  const live = sourceChange(resource)
  const blocked =
    resource.ref.uid !== change.uid
      ? `${resource.name} was deleted and created again since this dialog opened. Close it and look again.`
      : !live
        ? `${resource.name} no longer names an OCI artifact. Close this dialog and look again.`
        : live.current !== change.current
          ? `${change.field} changed since this dialog opened: it names ${live.current} now. Close it and look again.`
          : live.blocked
  const next = (live ?? change).next
  const parsed = parsePinned(value)
  const same = parsed.ok && parsed.reference === change.current
  const problem =
    value === ""
      ? ""
      : !parsed.ok
        ? parsed.message
        : same
          ? `This is the reference ${change.field} names now.`
          : ""
  const ready = parsed.ok && !same && blocked === ""
  const check = checked?.value === value ? checked.response : undefined
  const accepted =
    check !== undefined && check.outcome === DecisionOutcome.ADMITTED && !nothingSent(check)

  async function send(dryRun: boolean) {
    setStep({ kind: dryRun ? "checking" : "sending" })
    try {
      const response = await setSource.mutateAsync({
        kind: resource.kind === "schema" ? ResourceKind.SCHEMA : ResourceKind.MIGRATION,
        namespace: resource.namespace,
        name: resource.name,
        uid: change!.uid,
        fromReference: change!.current,
        reference: value,
        dryRun,
      })
      if (dryRun) {
        setChecked({ value, response })
        setStep({ kind: "edit" })
        return
      }
      void queryClient.invalidateQueries()
      setStep({ kind: "result", response })
    } catch (error) {
      const failure = ConnectError.from(error)
      if (dryRun) {
        // A check stores nothing, whatever became of it.
        setChecked({
          value,
          response: create(SetSourceResponseSchema, {
            outcome: lost(failure) ? DecisionOutcome.UNKNOWN : DecisionOutcome.REFUSED,
            dryRun: true,
            message: `${failureText(failure)} Nothing was stored.`,
          }),
        })
        setStep({ kind: "edit" })
      } else if (lost(failure)) {
        // The change may have reached the API server before the answer was
        // lost; only reading the resource back says.
        setStep({
          kind: "result",
          response: create(SetSourceResponseSchema, {
            outcome: DecisionOutcome.UNKNOWN,
            message: failure.rawMessage,
          }),
        })
      } else {
        setStep({ kind: "failed", message: failureText(failure) })
      }
    }
  }

  /** Reads the resource back after an answer that was lost. */
  async function readBack(response: SetSourceResponse) {
    setStep({ kind: "reading", response })
    try {
      const request = { namespace: resource.namespace, name: resource.name }
      const now =
        resource.kind === "schema"
          ? (await getSchema.mutateAsync(request)).schema?.source?.specReference
          : (await getMigration.mutateAsync(request)).migration?.source?.specReference
      void queryClient.invalidateQueries()
      setStep({ kind: "read", stored: now === value, now: now ?? "" })
    } catch (error) {
      setStep({ kind: "failed", message: failureText(error) })
    }
  }

  const busy = step.kind === "checking" || step.kind === "sending" || step.kind === "reading"
  const view = describe(step, resource, change)
  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => (open ? undefined : onClose())}
      title={view.title}
      titleTone={view.tone}
      where={where(context, resource.namespace)}
      as={session.data?.kubernetesUsername ?? "…"}
      busy={busy}
    >
      {step.kind === "edit" || step.kind === "checking" ? (
        <>
          <DialogDescription className="m-0 text-[13.5px] leading-normal text-fg2">
            Sets <code className="font-mono text-xs">{change.field}</code> to another artifact,
            pinned by digest, and changes nothing else: the verification policy, the registry
            credential, the transport and the apply policy stay as they are, and the new artifact is
            held to them.
          </DialogDescription>
          <dl className="m-0 grid gap-1 text-[13px]">
            <dt className="text-mute">Now</dt>
            <dd className="m-0 font-mono text-xs break-all">{change.current}</dd>
          </dl>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={inputId} className="text-sm font-medium">
              New reference
            </label>
            <input
              id={inputId}
              value={value}
              autoCapitalize="none"
              autoComplete="off"
              spellCheck={false}
              placeholder={pinnedForm}
              aria-invalid={problem ? true : undefined}
              aria-describedby={`${hintId} ${problemId}`}
              onChange={(event) => setValue(event.target.value)}
              className="min-h-10 w-full rounded-xl border border-line bg-card px-3 font-mono text-xs text-fg focus-visible:outline-2 focus-visible:outline-pri aria-invalid:border-red"
            />
            <span id={hintId} className="text-xs text-mute">
              A reference pinned by digest, {pinnedForm}.{" "}
              <code className="font-mono">
                ptah {resource.kind === "schema" ? "schema" : "migrations"} push --format reference
              </code>{" "}
              prints the one a push wrote.
            </span>
            <span id={problemId} className="text-xs break-words text-red">
              {problem}
            </span>
          </div>
          {blocked ? (
            <p role="alert" className="m-0 flex items-start gap-2 text-[13px]">
              <StatusGlyph tone="warn" size={20} />
              <span>{blocked}</span>
            </p>
          ) : null}
          <section aria-label="What happens next" className="flex flex-col gap-1.5">
            <h3 className="m-0 text-sm font-semibold">What happens next</h3>
            <ul className="m-0 flex list-disc flex-col gap-1 pl-5 text-[13px] leading-normal text-fg2">
              {next.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </section>
          {check ? <CheckAnswer response={check} /> : null}
          {step.kind === "checking" ? <Busy text="Asking the API server…" /> : null}
          <div className="flex flex-wrap justify-end gap-2">
            <Button onClick={onClose}>Cancel</Button>
            <Button disabled={!ready || step.kind === "checking"} onClick={() => void send(true)}>
              Check with the API server
            </Button>
            <Button
              variant="primary"
              disabled={!ready || !accepted || step.kind === "checking"}
              aria-describedby={gateId}
              onClick={() => void send(false)}
            >
              Change source
            </Button>
          </div>
          <span id={gateId} className="text-right text-xs text-mute">
            {accepted
              ? "The API server accepted this reference; nothing is stored until you change the source."
              : "Only a reference the API server accepted in a check can be sent."}
          </span>
        </>
      ) : null}
      {step.kind === "sending" ? <Busy text="Submitting…" /> : null}
      {step.kind === "reading" ? <Busy text="Reading it back…" /> : null}
      {step.kind === "result" || step.kind === "read" || step.kind === "failed" ? (
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
            <Button onClick={onClose}>Close</Button>
            {step.kind === "result" && step.response.outcome === DecisionOutcome.UNKNOWN ? (
              <Button variant="primary" onClick={() => void readBack(step.response)}>
                Check result
              </Button>
            ) : null}
          </div>
        </>
      ) : null}
    </ConfirmDialog>
  )
}

function Busy({ text }: { text: string }) {
  return (
    <p role="status" className="m-0 flex items-center gap-3 p-2 text-sm">
      <span className="box-border size-5 animate-[pt-spin_.8s_linear_infinite] rounded-full border-2 border-pri border-t-transparent" />
      {text}
    </p>
  )
}

/** What the API server said of a dry run, in its own words. */
function CheckAnswer({ response }: { response: SetSourceResponse }) {
  const accepted = response.outcome === DecisionOutcome.ADMITTED
  return (
    <section aria-label="What the API server says" className="flex flex-col gap-2">
      <span className="flex items-start gap-2.5">
        <StatusGlyph
          tone={accepted ? "ok" : response.outcome === DecisionOutcome.UNKNOWN ? "unk" : "bad"}
          size={22}
        />
        <span className="flex min-w-0 flex-col gap-1">
          <b className="text-[13.5px] font-semibold">{checkTitle(response)}</b>
          {response.message ? (
            <span
              role={accepted ? undefined : "alert"}
              className="text-[13px] break-words text-fg2"
            >
              {response.message}
            </span>
          ) : null}
          {accepted ? <span className="text-xs text-mute">Nothing was stored.</span> : null}
        </span>
      </span>
      {response.sourceYaml ? (
        <Terminal label="The source as it would be stored" text={response.sourceYaml} />
      ) : null}
    </section>
  )
}

interface View {
  title: string
  tone: "fg" | "green" | "amber" | "red" | "violet"
  text?: string
  serverWords?: string
}

function describe(step: Step, resource: ResourceView, change: SourceChange): View {
  const asked = { title: `Change the source of ${resource.name}?`, tone: "fg" as const }
  switch (step.kind) {
    case "failed":
      return { title: "The console could not send it", tone: "red", text: step.message }
    case "read":
      return step.stored
        ? {
            title: "Source changed",
            tone: "green",
            text: `The change was stored: ${change.field} names the new reference. The operator resolves it once it observes the change; this page follows it.`,
          }
        : {
            title: "Not changed",
            tone: "amber",
            text: `The change was not stored: ${change.field} names ${step.now || "no reference"}. Look at the resource again before you send it once more.`,
          }
    case "result":
      return outcome(step.response, resource)
    default:
      return asked
  }
}

function outcome(response: SetSourceResponse, resource: ResourceView): View {
  switch (response.outcome) {
    case DecisionOutcome.ADMITTED:
      if (!response.changed) {
        return { title: "Nothing to change", tone: "green", text: response.message }
      }
      return {
        title: "Source changed",
        tone: "green",
        text: response.suspended
          ? `${response.field} of ${resource.name} names the new artifact, at generation ${response.generation}. The resource is suspended: nothing resolves the new reference until it is resumed.`
          : `${response.field} of ${resource.name} names the new artifact, at generation ${response.generation}. The operator resolves it once it observes that generation, and this page follows it.`,
      }
    case DecisionOutcome.CHANGED:
      return { title: "It changed", tone: "amber", text: response.message }
    case DecisionOutcome.FORBIDDEN:
      return {
        title: "Kubernetes does not let you",
        tone: "red",
        text: "RBAC refused this identity. Nothing was changed.",
        serverWords: response.message,
      }
    case DecisionOutcome.REFUSED:
      return {
        title: "Refused",
        tone: "red",
        text: "The operator's admission or the API server refused it. Nothing was changed.",
        serverWords: response.message,
      }
    default:
      return {
        title: "Submission outcome unknown",
        tone: "violet",
        text: "The response was lost. Kubernetes may already have the change. Check before doing anything else.",
        serverWords: response.message,
      }
  }
}

/** The header over the API server's answer to a check. */
function checkTitle(response: SetSourceResponse) {
  switch (response.outcome) {
    case DecisionOutcome.ADMITTED:
      // The console found the reference already set and sent nothing.
      return nothingSent(response) ? "Nothing to change" : "The API server accepts this change"
    case DecisionOutcome.CHANGED:
      return "The resource changed"
    case DecisionOutcome.UNKNOWN:
      return "The API server did not answer"
    default:
      return "The API server refuses this change"
  }
}

/** An answer the console gave itself: the reference was set already, and nothing was sent. */
function nothingSent(response: SetSourceResponse) {
  return response.sourceYaml === "" && response.message !== ""
}

/**
 * Whether a failed call may have reached the API server: the answer was lost
 * on the way back, rather than refused before anything was sent.
 */
function lost(failure: ConnectError) {
  return [
    Code.Unknown,
    Code.DeadlineExceeded,
    Code.Unavailable,
    Code.Canceled,
    Code.Internal,
    Code.Aborted,
  ].includes(failure.code)
}

function failureText(error: unknown) {
  const failure = ConnectError.from(error)
  if (failure.code === Code.PermissionDenied || failure.code === Code.InvalidArgument) {
    return failure.rawMessage
  }
  if (failure.code === Code.Unauthenticated) {
    return "The session ended. Sign in again."
  }
  return `The console did not answer (${failure.rawMessage}). Look at the resource again before sending it once more.`
}
