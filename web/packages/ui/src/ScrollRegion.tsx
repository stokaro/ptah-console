import type { ReactNode } from "react"

export interface ScrollRegionProps {
  children: ReactNode
  /** What it holds; for a table, its caption. */
  label: string
  className?: string
}

/**
 * Content that may be wider than the page, such as a table, scrolling on its
 * own. It is a named stop in the tab order, so a keyboard reaches its hidden
 * columns even when nothing inside it takes focus. It is a group rather than a
 * region: a page holds several tables, often inside a card of the same name,
 * and none of them belongs in the list of landmarks.
 */
export function ScrollRegion({ children, label, className = "" }: ScrollRegionProps) {
  return (
    <div
      role="group"
      aria-label={label}
      tabIndex={0}
      className={`overflow-x-auto rounded-md focus-visible:outline-2 focus-visible:outline-pri ${className}`}
    >
      {children}
    </div>
  )
}
