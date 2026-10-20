import { Code, ConnectError } from "@connectrpc/connect"
import { skipToken, useQuery } from "@connectrpc/connect-query"

import { DecisionService } from "../gen/ptah/console/v1/decisions_pb"
import { InfoService, Mode } from "../gen/ptah/console/v1/info_pb"
import { useLive } from "./live"
import {
  Access,
  Answer,
  MigrationService,
  SchemaService,
  ScopeService,
  type Capabilities,
  type NamespaceScope,
} from "../gen/ptah/console/v1/resources_pb"
import { SessionService } from "../gen/ptah/console/v1/session_pb"
import { fromMigration, fromSchema, type ResourceKind, type ResourceView } from "../model/resource"

export function useSession() {
  return useQuery(SessionService.method.getSession, {})
}

/** What the console is: its version, and whether it runs local or hosted. */
export function useInfo() {
  return useQuery(InfoService.method.getInfo, {})
}

/** Whether people sign in to this console with an account; undefined until known. */
export function useHosted(): boolean | undefined {
  const info = useInfo()
  return info.data ? info.data.mode === Mode.HOSTED : undefined
}

/** The namespaces the console may show, and what the session may do in each. */
export function useScope() {
  const query = useQuery(ScopeService.method.listNamespaces, {})
  const scopes: NamespaceScope[] = query.data?.namespaces ?? []
  const readable = scopes.filter((scope) => scope.access === Access.OK).map((scope) => scope.name)
  return { ...query, scopes, readable }
}

export type Capability = keyof Omit<Capabilities, "$typeName" | "$unknown">

/** Whether RBAC lets the session do something in a namespace. Unknown counts as no. */
export function can(scopes: NamespaceScope[], namespace: string, capability: Capability) {
  const scope = scopes.find((s) => s.name === namespace)
  return scope?.capabilities?.[capability] === Answer.ALLOWED
}

/** Whether the session may do something in any namespace it sees. */
export function canAnywhere(scopes: NamespaceScope[], capability: Capability) {
  return scopes.some((scope) => scope.capabilities?.[capability] === Answer.ALLOWED)
}

/** Whether a page offers a decision, and if not, why not. */
export type Offer = "allowed" | "read-only" | "denied" | "offline"

/** The text a page shows in place of a decision a read-only console refuses. */
export const readOnlyText = "This console runs read-only: it sends no decision."

/** The text a page shows in place of a decision while the cluster is out of reach. */
export const offlineText =
  "The console lost touch with the cluster. Decisions wait until it is back; this page reconnects on its own."

/** What a page shows in place of a decision it does not offer, or nothing. */
export function offerText(offer: Offer, denied: string) {
  return { allowed: "", "read-only": readOnlyText, offline: offlineText, denied }[offer]
}

/**
 * Says whether a page should offer a decision: not in a read-only console,
 * and not where RBAC says no. The server decides either way; this only keeps
 * a button off a page where pressing it can only be refused.
 */
export function useOffer() {
  const session = useSession()
  const scope = useScope()
  const live = useLive()
  return (namespace: string | undefined, capability: Capability): Offer => {
    if (session.data?.readOnly) {
      return "read-only"
    }
    if (live.status === "lost") {
      return "offline"
    }
    const allowed =
      namespace === undefined
        ? canAnywhere(scope.scopes, capability)
        : can(scope.scopes, namespace, capability)
    return allowed ? "allowed" : "denied"
  }
}

/** Every schema and migration in the readable namespaces. */
export function useResources() {
  const scope = useScope()
  const input = scope.readable.length > 0 ? { namespaces: scope.readable } : skipToken
  const schemas = useQuery(SchemaService.method.listSchemas, input)
  const migrations = useQuery(MigrationService.method.listMigrations, input)
  const resources: ResourceView[] = [
    ...(schemas.data?.schemas ?? []).map(fromSchema),
    ...(migrations.data?.migrations ?? []).map(fromMigration),
  ]
  const truncated = [...(schemas.data?.namespaces ?? []), ...(migrations.data?.namespaces ?? [])]
    .filter((result) => result.truncated)
    .map((result) => result.namespace)
  return {
    scope,
    resources,
    truncated: [...new Set(truncated)],
    loading:
      scope.isPending || (scope.readable.length > 0 && (schemas.isPending || migrations.isPending)),
    error: scope.error ?? schemas.error ?? migrations.error,
  }
}

export function useDecisions() {
  const scope = useScope()
  return {
    scope,
    decisions: useQuery(
      DecisionService.method.listDecisions,
      scope.readable.length > 0 ? { namespaces: scope.readable } : skipToken
    ),
  }
}

/**
 * One schema or migration: from the namespace's list when the identity may
 * list it, otherwise read by name, which is what a reviewer granted only
 * get, opening a link someone sent, may do.
 */
export function useResource(kind: ResourceKind | undefined, namespace: string, name: string) {
  const list = useResources()
  const listed = list.resources.find(
    (resource) =>
      resource.kind === kind && resource.namespace === namespace && resource.name === name
  )
  const direct = !listed && !list.loading
  const schema = useQuery(
    SchemaService.method.getSchema,
    direct && kind === "schema" ? { namespace, name } : skipToken
  )
  const migration = useQuery(
    MigrationService.method.getMigration,
    direct && kind === "migration" ? { namespace, name } : skipToken
  )
  const read = schema.data?.schema
    ? fromSchema(schema.data.schema)
    : migration.data?.migration
      ? fromMigration(migration.data.migration)
      : undefined
  const failure = schema.error ?? migration.error
  const notFound = failure && ConnectError.from(failure).code === Code.NotFound
  return {
    scope: list.scope,
    resource: listed ?? read,
    loading: list.loading || (direct && (schema.isLoading || migration.isLoading)),
    error: listed ? undefined : notFound ? undefined : failure,
  }
}
