export type ThemeChoice = "system" | "light" | "dark"

/** The themes a viewer may choose, in the order a switch lists them. */
export const themes: { value: ThemeChoice; label: string }[] = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
]

const storageKey = "ptah-console-theme"

/** The stored choice, or "system" when there is none or storage is blocked. */
export function readTheme(): ThemeChoice {
  try {
    const stored = window.localStorage.getItem(storageKey)
    return stored === "light" || stored === "dark" ? stored : "system"
  } catch {
    return "system"
  }
}

export function storeTheme(choice: ThemeChoice) {
  try {
    if (choice === "system") {
      window.localStorage.removeItem(storageKey)
    } else {
      window.localStorage.setItem(storageKey, choice)
    }
  } catch {
    // A blocked storage only means the choice lasts for this page.
  }
}

function systemDark() {
  return (
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches
  )
}

/** Sets data-theme on the document to the theme the choice resolves to. */
export function applyTheme(choice: ThemeChoice, root: HTMLElement = document.documentElement) {
  const dark = choice === "dark" || (choice === "system" && systemDark())
  root.setAttribute("data-theme", dark ? "dark" : "light")
}

/**
 * Applies a choice, and keeps applying "system" as the system's preference
 * changes. Returns what stops following it.
 */
export function followTheme(choice: ThemeChoice): () => void {
  applyTheme(choice)
  if (choice !== "system" || typeof window.matchMedia !== "function") {
    return () => {}
  }
  const media = window.matchMedia("(prefers-color-scheme: dark)")
  const follow = () => applyTheme("system")
  media.addEventListener("change", follow)
  return () => media.removeEventListener("change", follow)
}
