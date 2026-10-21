import type { ResourceView } from "../../model/resource"

/**
 * Where the full console opens a resource: a schema on its own page; a
 * migration on the focused view's page until the console has one.
 */
export function resourceLink(resource: ResourceView) {
  if (resource.kind === "schema") {
    return {
      to: "/console/schemas/$namespace/$name" as const,
      params: { namespace: resource.namespace, name: resource.name },
    }
  }
  return {
    to: "/releases/$namespace/$kind/$name" as const,
    params: { namespace: resource.namespace, kind: resource.kind, name: resource.name },
  }
}
