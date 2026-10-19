import { createConnectTransport } from "@connectrpc/connect-web"

/** The Connect API, served by the same origin under /api. */
export const apiBaseUrl = "/api"

export function createApiTransport() {
  return createConnectTransport({ baseUrl: apiBaseUrl })
}
