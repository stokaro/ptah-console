import { createContext, useContext } from "react"

/**
 * The namespaces the pages below read. The full console provides the
 * viewer's choice, or null while it cannot yet tell whose choice to read.
 * Elsewhere, as in the focused view, nothing is provided and every readable
 * namespace is read.
 */
export const NamespaceSelection = createContext<string[] | null | undefined>(undefined)

/**
 * The namespaces to read: the choice when one is provided, every readable
 * one otherwise, and null while the choice is not known yet.
 */
export function useSelectedNamespaces(readable: string[]): string[] | null {
  const provided = useContext(NamespaceSelection)
  return provided === undefined ? readable : provided
}
