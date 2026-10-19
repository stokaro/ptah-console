export interface WordmarkProps {
  /** The line under the name, such as the console's mode. */
  caption?: string
}

/** The product mark: a square and the name, with an optional caption. */
export function Wordmark({ caption = "focus" }: WordmarkProps) {
  return (
    <span className="inline-flex items-center gap-2.5">
      <span
        aria-hidden="true"
        className="size-[30px] rounded-[9px] bg-linear-135 from-pri to-teal"
      />
      <span className="flex flex-col leading-none">
        <span className="font-display text-[19px] font-bold tracking-[-0.04em]">ptah</span>
        <span className="mt-1 font-mono text-[9px] font-medium tracking-[0.18em] text-mute uppercase">
          {caption}
        </span>
      </span>
    </span>
  )
}
