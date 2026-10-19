import type { ReactNode } from "react"

export interface CardProps {
  children: ReactNode
  className?: string
  /** The small uppercase label that names the section. */
  title?: string
  /** Accessible name when the section has no visible title. */
  label?: string
}

/** A section of a page: a bordered surface with an optional label. */
export function Card({ children, className = "", title, label }: CardProps) {
  return (
    <section
      aria-label={label ?? title}
      className={`flex flex-col gap-2.5 rounded-[18px] border border-line bg-card p-[18px] shadow-card ${className}`}
    >
      {title ? <Eyebrow>{title}</Eyebrow> : null}
      {children}
    </section>
  )
}

/** The small uppercase monospace label above a section or a heading. */
export function Eyebrow({ children }: { children: ReactNode }) {
  return (
    <span className="font-mono text-[10px] font-medium tracking-[0.16em] text-mute uppercase">
      {children}
    </span>
  )
}
