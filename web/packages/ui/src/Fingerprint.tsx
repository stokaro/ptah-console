import { copyWords, useCopy } from "./copy"
import { shortDigest } from "./digest"

/** A digest or fingerprint, shortened, with the whole value one click away. */
export function Fingerprint({ value, label = "fingerprint" }: { value: string; label?: string }) {
  const [state, copy] = useCopy()
  return (
    <span className="inline-flex flex-wrap items-center gap-2 font-mono text-xs text-mute">
      <span title={value}>
        {label} {shortDigest(value, 8)}
      </span>
      <button
        type="button"
        onClick={() => void copy(value)}
        aria-label={`Copy ${label} ${value}`}
        className="cursor-pointer rounded-md border border-line bg-card px-2 py-0.5 font-sans text-[11px] text-fg"
      >
        Copy
      </button>
      <span role="status" className="font-sans">
        {copyWords[state]}
      </span>
    </span>
  )
}
