/** Plain text from a program, in a terminal-colored block. Never interpreted. */
export function Terminal({ text, label }: { text: string; label: string }) {
  return (
    <pre
      aria-label={label}
      className="m-0 overflow-x-auto rounded-[10px] bg-term p-3 font-mono text-xs leading-[1.7] break-words whitespace-pre-wrap text-termfg"
    >
      {text}
    </pre>
  )
}
