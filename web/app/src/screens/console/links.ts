import type { ResourceView } from "../../model/resource"

/** Where the full console opens a resource: on its own page. */
export function resourceLink(resource: ResourceView) {
  return resource.kind === "schema"
    ? {
        to: "/console/schemas/$namespace/$name" as const,
        params: { namespace: resource.namespace, name: resource.name },
      }
    : {
        to: "/console/migrations/$namespace/$name" as const,
        params: { namespace: resource.namespace, name: resource.name },
      }
}
