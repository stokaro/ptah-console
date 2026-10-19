import { useState } from "react"

export type CopyState = "idle" | "copied" | "failed"

/** Copies text to the clipboard, and remembers whether it worked. */
export function useCopy(): [CopyState, (text: string) => Promise<void>] {
  const [state, setState] = useState<CopyState>("idle")
  return [
    state,
    async (text: string) => {
      try {
        await navigator.clipboard.writeText(text)
        setState("copied")
      } catch {
        setState("failed")
      }
    },
  ]
}

export const copyWords: Record<CopyState, string> = {
  idle: "",
  copied: "Copied.",
  failed: "The browser did not allow copying.",
}
