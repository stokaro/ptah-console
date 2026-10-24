import { createContext } from "react"

/**
 * Opens the welcome again, where the full console offers it; undefined
 * elsewhere. Kept apart from the welcome so a page that only offers the way
 * back to it does not load it.
 */
export const WelcomeControl = createContext<(() => void) | undefined>(undefined)
