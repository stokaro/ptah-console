import { ConnectError } from "@connectrpc/connect"
import { useMutation } from "@connectrpc/connect-query"
import { Button, Card, Eyebrow, Segmented, Select, StatusGlyph, Terminal } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { useId, useState, type ReactNode } from "react"

import { can, offerText, useOffer, useScope, useSession } from "../../api/resources"
import { AuthoringService, type CreateResult } from "../../gen/ptah/console/v1/authoring_pb"
import { DecisionOutcome } from "../../gen/ptah/console/v1/decisions_pb"
import {
  applyPolicies,
  draftKey,
  emptyForm,
  engines,
  missing,
  sourceTemplates,
  toDraft,
  type AuthoringKind,
  type DraftForm,
  type SourceTemplate,
} from "../../model/draft"
import { Loading } from "../common"

export interface NewResourceProps {
  kind: AuthoringKind
  template: SourceTemplate
  /** The kind or the template changed; the page keeps the rest of the form. */
  onChoose: (choice: { kind: AuthoringKind; template: SourceTemplate }) => void
}

const kinds: { value: AuthoringKind; label: string; resource: string }[] = [
  { value: "schema", label: "Schema", resource: "PtahSchema" },
  { value: "migration", label: "Migration", resource: "PtahMigration" },
]

const field =
  "min-h-10 w-full rounded-xl border border-line bg-card px-3 text-sm text-fg focus-visible:outline-2 focus-visible:outline-pri"

/**
 * A new PtahSchema or PtahMigration from typed fields. The console builds the
 * object from the fields alone and sends no document a person wrote; the API
 * server checks it first with a dry run, and only the draft that check
 * accepted can be created, as the session's identity.
 */
export function NewResource({ kind, template, onChoose }: NewResourceProps) {
  const scope = useScope()
  const session = useSession()
  const offer = useOffer()
  const queryClient = useQueryClient()
  const capability = kind === "schema" ? "createSchemas" : "createMigrations"
  const allowed = scope.scopes
    .filter((s) => can(scope.scopes, s.name, capability))
    .map((s) => s.name)
  const [form, setForm] = useState<DraftForm>(() => emptyForm(kind, template, allowed[0] ?? ""))
  const [checked, setChecked] = useState<{ key: string; result: CreateResult }>()
  const [created, setCreated] = useState<CreateResult>()
  const schema = useMutation(AuthoringService.method.createSchema)
  const migration = useMutation(AuthoringService.method.createMigration)
  const sending = schema.isPending || migration.isPending

  if (scope.isPending) {
    return <Loading what="where this identity may create resources" />
  }
  // The kind and the template come from the address; the rest is the form's.
  const current: DraftForm = {
    ...form,
    kind,
    template,
    namespace: form.namespace || allowed[0] || "",
  }
  const resource = kinds.find((k) => k.value === kind)!.resource
  const blank = missing(current)
  const key = draftKey(current)
  const offered = offer(current.namespace || undefined, capability)
  const change = (patch: Partial<DraftForm>) => setForm({ ...current, ...patch })
  const send = async (dryRun: boolean) => {
    const request = { draft: toDraft(current), dryRun }
    try {
      const answer =
        kind === "schema" ? await schema.mutateAsync(request) : await migration.mutateAsync(request)
      const result = answer.result!
      if (dryRun) {
        setChecked({ key, result })
        return
      }
      setCreated(result)
      if (result.outcome === DecisionOutcome.ADMITTED) {
        await queryClient.invalidateQueries()
      }
    } catch (error) {
      const failure = ConnectError.from(error)
      const result = {
        outcome: DecisionOutcome.REFUSED,
        message: failure.rawMessage,
        dryRun,
      } as CreateResult
      if (dryRun) {
        setChecked({ key, result })
      } else {
        setCreated(result)
      }
    }
  }
  const checkPassed =
    checked?.key === key && checked.result.outcome === DecisionOutcome.ADMITTED && !created
  const identity = session.data?.kubernetesUsername

  return (
    <>
      <section className="flex flex-col gap-1.5">
        <Eyebrow>Authoring</Eyebrow>
        <h1 className="m-0 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
          New {resource}
        </h1>
        <span className="max-w-[760px] text-[13.5px] text-fg2">
          The console builds the object from these fields and sends nothing else. The API server
          checks it first, with the operator&apos;s CRD and admission, and stores nothing; only the
          draft it accepted can be created, as {identity ?? "this identity"}.
        </span>
      </section>

      {allowed.length === 0 ? (
        <Card title="Nowhere to create one">
          <span className="text-sm text-fg2">
            RBAC lets this identity create a {resource} in none of the namespaces the console reads.
          </span>
        </Card>
      ) : (
        <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
          <form
            aria-label={`New ${resource}`}
            className="flex min-w-0 flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault()
              void send(true)
            }}
          >
            <Card title="What">
              <Choice label="Kind">
                <Segmented
                  label="Kind"
                  value={kind}
                  onChange={(next) => onChoose({ kind: next, template })}
                  options={kinds.map(({ value, label }) => ({ value, label }))}
                />
              </Choice>
              <Choice label="From">
                <Segmented
                  label="Source"
                  value={template}
                  onChange={(next) => onChoose({ kind, template: next })}
                  options={sourceTemplates}
                />
              </Choice>
              <Text
                label="Namespace"
                about="Where RBAC lets this identity create one."
                select={allowed}
                value={current.namespace}
                onChange={(namespace) => change({ namespace })}
              />
              <Text
                label="Name"
                about="Lowercase letters, digits, hyphens and dots."
                value={current.name}
                onChange={(name) => change({ name })}
              />
            </Card>

            <Card title="Database">
              <Text
                label="Engine"
                select={[...engines]}
                value={current.engine}
                onChange={(engine) => change({ engine: engine as DraftForm["engine"] })}
              />
              <Choice label="Who else manages it">
                <Segmented
                  label="Who else manages it"
                  value={current.coordination}
                  onChange={(coordination) => change({ coordination })}
                  options={[
                    { value: "key", label: "Resources in this namespace" },
                    { value: "realm", label: "A realm an administrator granted" },
                  ]}
                />
              </Choice>
              {current.coordination === "key" ? (
                <Text
                  label="Coordination key"
                  about="Every resource in the namespace that reaches this database uses the same key."
                  value={current.coordinationKey}
                  onChange={(coordinationKey) => change({ coordinationKey })}
                />
              ) : (
                <Text
                  label="Realm"
                  about="The PtahRealm that grants this database to the namespace."
                  value={current.realm}
                  onChange={(realm) => change({ realm })}
                />
              )}
              <div className="grid gap-3 sm:grid-cols-2">
                <Text
                  label="Connection Secret"
                  about="The console never reads it."
                  value={current.secretName}
                  onChange={(secretName) => change({ secretName })}
                />
                <Text
                  label="Secret key"
                  value={current.secretKey}
                  onChange={(secretKey) => change({ secretKey })}
                />
              </div>
            </Card>

            <Card title={kind === "schema" ? "Desired schema" : "Migrations"}>
              <SourceFields form={current} change={change} />
            </Card>

            <Card title="Applying">
              <Choice label="Apply">
                <Segmented
                  label="Apply"
                  value={current.apply}
                  onChange={(apply) => change({ apply })}
                  options={applyPolicies.map(({ value, label }) => ({ value, label }))}
                />
              </Choice>
              <span className="text-xs text-mute">
                {applyPolicies.find((policy) => policy.value === current.apply)?.about}
              </span>
            </Card>

            <div className="flex flex-wrap items-center gap-2.5">
              <Button type="submit" disabled={blank.length > 0 || offered !== "allowed" || sending}>
                {sending ? "Sending…" : "Check with the API server"}
              </Button>
              <Button
                variant="primary"
                disabled={!checkPassed || offered !== "allowed" || sending}
                onClick={() => void send(false)}
              >
                Create {resource}
              </Button>
              <span role="status" className="text-xs text-mute">
                {offered !== "allowed"
                  ? offerText(
                      offered,
                      `This identity may not create a ${resource} in ${current.namespace}.`
                    )
                  : blank.length > 0
                    ? `Still empty: ${blank.join(", ")}.`
                    : checked && checked.key !== key && !created
                      ? "The draft changed since the check; check it again."
                      : ""}
              </span>
            </div>
          </form>

          <div className="flex min-w-0 flex-col gap-4">
            {created ? (
              <Outcome result={created} resource={resource} kind={kind} />
            ) : checked ? (
              <Outcome
                result={checked.result}
                resource={resource}
                kind={kind}
                stale={checked.key !== key}
              />
            ) : (
              <Card title="What the API server says">
                <span className="text-sm text-fg2">
                  Nothing is sent until you check. A check stores nothing.
                </span>
              </Card>
            )}
          </div>
        </div>
      )}
    </>
  )
}

function SourceFields({
  form,
  change,
}: {
  form: DraftForm
  change: (patch: Partial<DraftForm>) => void
}) {
  switch (form.template) {
    case "oci":
      return (
        <>
          <Text
            label="Artifact"
            about="An oci:// reference, by tag or digest."
            value={form.ociRef}
            onChange={(ociRef) => change({ ociRef })}
          />
          <div className="grid gap-3 sm:grid-cols-2">
            <Text
              label="Verification policy"
              about="The ConfigMap the artifact is held to."
              value={form.policyName}
              onChange={(policyName) => change({ policyName })}
            />
            <Text
              label="Policy key"
              value={form.policyKey}
              onChange={(policyKey) => change({ policyKey })}
            />
          </div>
        </>
      )
    case "configmap":
      return (
        <>
          <Text
            label="ConfigMap"
            about="In the same namespace. Whoever may write it may change the desired state."
            value={form.configMapName}
            onChange={(configMapName) => change({ configMapName })}
          />
          <div className="grid gap-3 sm:grid-cols-2">
            <Text
              label="Key"
              value={form.configMapKey}
              onChange={(configMapKey) => change({ configMapKey })}
            />
            <Text
              label="File name"
              about="Its extension tells Ptah the format."
              value={form.configMapPath}
              onChange={(configMapPath) => change({ configMapPath })}
            />
          </div>
        </>
      )
    case "git":
      return (
        <>
          <Text
            label="Repository"
            about="An https:// or ssh URL."
            value={form.gitUrl}
            onChange={(gitUrl) => change({ gitUrl })}
          />
          <Choice label="Ref">
            <Segmented
              label="Ref"
              value={form.gitRefKind}
              onChange={(gitRefKind) => change({ gitRefKind })}
              options={[
                { value: "branch", label: "Branch" },
                { value: "tag", label: "Tag" },
                { value: "commit", label: "Commit" },
              ]}
            />
          </Choice>
          <div className="grid gap-3 sm:grid-cols-2">
            <Text
              label={
                form.gitRefKind === "branch"
                  ? "Branch"
                  : form.gitRefKind === "tag"
                    ? "Tag"
                    : "Commit"
              }
              value={form.gitRef}
              onChange={(gitRef) => change({ gitRef })}
            />
            <Text
              label="Path"
              about="The file in the repository."
              value={form.gitPath}
              onChange={(gitPath) => change({ gitPath })}
            />
          </div>
          <Text
            label="Credential Secret"
            about="Optional, for a private repository. The console never reads it."
            value={form.gitSecret}
            onChange={(gitSecret) => change({ gitSecret })}
          />
        </>
      )
  }
}

function Choice({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-sm font-medium">{label}</span>
      {children}
    </div>
  )
}

function Text({
  label,
  about,
  value,
  onChange,
  select,
}: {
  label: string
  about?: string
  value: string
  onChange: (value: string) => void
  select?: string[]
}) {
  const id = useId()
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      {select ? (
        <Select
          id={id}
          value={value}
          onChange={onChange}
          options={select.map((option) => ({ value: option, label: option }))}
        />
      ) : (
        <input
          id={id}
          value={value}
          autoCapitalize="none"
          spellCheck={false}
          onChange={(event) => onChange(event.target.value)}
          className={`${field} font-mono`}
        />
      )}
      {about ? <span className="text-xs text-mute">{about}</span> : null}
    </div>
  )
}

function Outcome({
  result,
  resource,
  kind,
  stale = false,
}: {
  result: CreateResult
  resource: string
  kind: AuthoringKind
  stale?: boolean
}) {
  const admitted = result.outcome === DecisionOutcome.ADMITTED
  const unknown = result.outcome === DecisionOutcome.UNKNOWN
  const title = result.dryRun
    ? admitted
      ? `The API server accepts this ${resource}`
      : `The API server refuses this ${resource}`
    : admitted
      ? `Created ${resource} ${result.ref?.name}`
      : unknown
        ? "The answer was lost"
        : `${resource} not created`
  return (
    <Card title="What the API server says">
      <span className="flex items-start gap-2.5">
        <StatusGlyph tone={admitted ? "ok" : unknown ? "unk" : "bad"} size={22} />
        <span className="flex min-w-0 flex-col gap-1">
          <b className="text-[13.5px] font-semibold">{title}</b>
          {result.message ? (
            <span
              role={admitted ? undefined : "alert"}
              className="text-[13px] break-words text-fg2"
            >
              {result.message}
            </span>
          ) : null}
          {result.dryRun && admitted ? (
            <span className="text-xs text-mute">
              Nothing was stored. {stale ? "The draft changed since; check it again." : ""}
            </span>
          ) : null}
        </span>
      </span>
      {!result.dryRun && admitted && result.ref ? (
        <Link
          to={
            kind === "schema"
              ? "/console/schemas/$namespace/$name"
              : "/console/migrations/$namespace/$name"
          }
          params={{ namespace: result.ref.namespace, name: result.ref.name }}
          className="text-sm font-medium text-pri"
        >
          Open {result.ref.namespace}/{result.ref.name}
        </Link>
      ) : null}
      {result.yaml ? (
        <Terminal
          label={result.dryRun ? "What would be created" : "What was created"}
          text={result.yaml}
        />
      ) : null}
    </Card>
  )
}
