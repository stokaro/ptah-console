import { StatusGlyph } from "./StatusGlyph"
import { toneStyle, type Tone } from "./tone"

export interface Step {
  label: string
  tone: Tone
  /** Why the step stands where it does. */
  detail?: string
}

/** A resource's lifecycle as a row of steps, each a glyph and a name. */
export function StepRail({ steps, label = "Progress" }: { steps: Step[]; label?: string }) {
  return (
    <ol
      aria-label={label}
      className="m-0 grid list-none gap-1 p-0"
      style={{ gridTemplateColumns: `repeat(${Math.max(steps.length, 1)}, minmax(0, 1fr))` }}
    >
      {steps.map((step, index) => (
        <li
          key={step.label}
          className="relative flex flex-col items-center gap-1.5"
          title={step.detail || undefined}
        >
          <span className="relative z-10">
            <StatusGlyph tone={step.tone} size={28} />
          </span>
          {index < steps.length - 1 ? (
            <span
              aria-hidden="true"
              className={`absolute top-[13px] right-[calc(-50%+16px)] left-[calc(50%+16px)] h-0.5 ${step.tone === "ok" ? "bg-green" : "bg-line"}`}
            />
          ) : null}
          <span className="text-center text-xs text-fg2">
            {step.label}
            <span className="sr-only">: {toneStyle(step.tone).label}</span>
          </span>
        </li>
      ))}
    </ol>
  )
}
