import type { Step } from "./StepRail"
import { toneStyle } from "./tone"

const dots = {
  ok: "bg-green",
  warn: "bg-amber",
  bad: "bg-red",
  unk: "bg-violet",
  run: "bg-pri",
  off: "bg-line",
} as const

/**
 * A lifecycle as a row of small dots, for a table row where the full step
 * rail does not fit. A dot's color is backed by a shape where it matters: a
 * failed or unknown step is square, so the row does not rely on color alone,
 * and the whole row reads as one sentence to assistive technology.
 */
export function StepDots({ steps, label }: { steps: Step[]; label: string }) {
  const sentence = steps.map((step) => `${step.label}: ${toneStyle(step.tone).label}`).join(", ")
  return (
    <span
      role="img"
      aria-label={`${label}. ${sentence}`}
      className="inline-flex items-center gap-1"
    >
      {steps.map((step) => (
        <span
          key={step.label}
          title={`${step.label}: ${toneStyle(step.tone).label}`}
          className={`block size-2 ${step.tone === "bad" || step.tone === "unk" ? "rounded-[1px]" : "rounded-full"} ${dots[step.tone]}`}
        />
      ))}
    </span>
  )
}
