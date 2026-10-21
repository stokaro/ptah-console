import type { ResourceView } from "../../model/resource"

/**
 * Where the full console opens a resource. Until its own detail pages exist,
 * that is the focused view's page for the release.
 */
export function resourceLink(resource: ResourceView) {
  return {
    to: "/releases/$namespace/$kind/$name" as const,
    params: { namespace: resource.namespace, kind: resource.kind, name: resource.name },
  }
}
