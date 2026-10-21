export interface SegmentedOption<T extends string> {
  value: T
  label: string
  /** How many items picking this option shows; omitted when not counted. */
  count?: number
}

export interface SegmentedProps<T extends string> {
  /** What the group chooses, for assistive technology. */
  label: string
  options: SegmentedOption<T>[]
  value: T
  onChange: (value: T) => void
}

/**
 * A row of mutually exclusive filter buttons, each with an optional count.
 * The pressed button is the current choice; the group is named so a reader
 * knows what it narrows.
 */
export function Segmented<T extends string>({
  label,
  options,
  value,
  onChange,
}: SegmentedProps<T>) {
  return (
    <div
      role="group"
      aria-label={label}
      className="flex flex-wrap gap-0.5 rounded-[11px] border border-line bg-card p-[3px]"
    >
      {options.map((option) => {
        const pressed = option.value === value
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={pressed}
            onClick={() => onChange(option.value)}
            className={`inline-flex h-[30px] cursor-pointer items-center gap-1.5 rounded-lg border-0 px-2.5 text-xs font-medium whitespace-nowrap ${pressed ? "bg-pribg text-pri" : "bg-transparent text-fg2"}`}
          >
            {option.label}
            {/* A space keeps the count a word of its own in the button's name. */}
            {option.count === undefined ? null : " "}
            {option.count === undefined ? null : (
              <span className={`font-mono text-[11px] ${pressed ? "text-pri" : "text-mute"}`}>
                {option.count}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}
