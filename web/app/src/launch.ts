import { createClient, type Transport } from "@connectrpc/connect"

import { SessionService } from "./gen/ptah/console/v1/session_pb"

const launchToken = /(?:^#|&)t=([A-Za-z0-9_-]+)/

/**
 * Reads the launch token ptah-console local put in the URL fragment, removes
 * it from the address bar and history before anything else happens, and
 * trades it for a session cookie. Returns false when there was no token.
 */
export async function exchangeLaunchToken(
  transport: Transport,
  location: Location,
  history: History
) {
  const match = launchToken.exec(location.hash)
  if (!match) {
    return false
  }
  history.replaceState(null, "", location.pathname + location.search)
  await createClient(SessionService, transport).exchangeLaunchToken({ token: match[1] })
  return true
}
