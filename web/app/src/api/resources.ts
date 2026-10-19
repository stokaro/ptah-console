import { skipToken, useQuery } from "@connectrpc/connect-query"

import { DecisionService } from "../gen/ptah/console/v1/decisions_pb"
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
import { fromMigration, fromSchema, type ResourceView } from "../model/resource"

export function useSession() {
  return useQuery(SessionService.method.getSession, {})
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
  return {
    scope,
    resources,
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
