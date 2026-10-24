import type { SelectHTMLAttributes } from "react"

export interface SelectOption {
  value: string
  label: string
}

export interface SelectProps extends Omit<
  SelectHTMLAttributes<HTMLSelectElement>,
  "onChange" | "value"
> {
  value: string
  options: SelectOption[]
  onChange: (value: string) => void
}

/**
 * One choice of several, as the platform's own select: a keyboard, a screen
 * reader and a phone each get the control they know. Label it with a
 * <label htmlFor> or aria-label.
 */
export function Select({ value, options, onChange, className = "", ...props }: SelectProps) {
  return (
    <select
      {...props}
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className={`min-h-10 w-full rounded-xl border border-line bg-card px-3 text-sm text-fg focus-visible:outline-2 focus-visible:outline-pri ${className}`}
    >
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  )
}
