import { useEffect, useState } from "react"

/** The accents a viewer may choose. The states keep their own colors. */
export const accents = [
  { value: "indigo", label: "Indigo" },
  { value: "teal", label: "Teal" },
  { value: "graphite", label: "Graphite" },
] as const

export type AccentChoice = (typeof accents)[number]["value"]

const storageKey = "ptah-console-accent"

function isAccent(value: string | null): value is AccentChoice {
  return accents.some((accent) => accent.value === value)
}

/** The stored choice, or indigo when there is none or storage is blocked. */
export function readAccent(): AccentChoice {
  try {
    const stored = window.localStorage.getItem(storageKey)
    return isAccent(stored) ? stored : "indigo"
  } catch {
    return "indigo"
  }
}

function storeAccent(choice: AccentChoice) {
  try {
    if (choice === "indigo") {
      window.localStorage.removeItem(storageKey)
    } else {
      window.localStorage.setItem(storageKey, choice)
    }
  } catch {
    // A blocked storage only means the choice lasts for this page.
  }
}

/** Sets data-accent on the document; indigo is the tokens' own default. */
export function applyAccent(choice: AccentChoice, root: HTMLElement = document.documentElement) {
  if (choice === "indigo") {
    root.removeAttribute("data-accent")
  } else {
    root.setAttribute("data-accent", choice)
  }
}

/** The accent choice, applied and stored. */
export function useAccent(): [AccentChoice, (choice: AccentChoice) => void] {
  const [choice, setChoice] = useState<AccentChoice>(readAccent)
  useEffect(() => applyAccent(choice), [choice])
  return [
    choice,
    (next) => {
      storeAccent(next)
      setChoice(next)
    },
  ]
}
