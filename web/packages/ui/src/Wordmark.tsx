export interface WordmarkProps {
  /** The line under the name, such as the console's mode. */
  caption?: string
}

/** The product mark: a square and the name, with an optional caption. */
export function Wordmark({ caption = "console" }: WordmarkProps) {
  return (
    <span className="inline-flex items-center gap-2.5">
      <span
        aria-hidden="true"
        className="size-7 rounded-lg bg-linear-to-br from-indigo-600 to-teal-600"
      />
      <span className="flex flex-col leading-none">
        <span className="text-lg font-bold tracking-tight">ptah</span>
        <span className="mt-1 text-[9px] font-medium tracking-[0.18em] text-slate-500 uppercase">
          {caption}
        </span>
      </span>
    </span>
  )
}
