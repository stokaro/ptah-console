/** The icons the console draws beside a label. Each is decoration: the label beside it says what it is. */
export type IconName =
  | "overview"
  | "resources"
  | "activity"
  | "safety"
  | "preflight"
  | "connections"
  | "focus"
  | "search"

const paths: Record<IconName, string> = {
  overview: "M2.5 2.5h4.5v4.5h-4.5zM9 2.5h4.5v4.5h-4.5zM2.5 9h4.5v4.5h-4.5zM9 9h4.5v4.5h-4.5z",
  resources: "M5.5 4h8M5.5 8h8M5.5 12h8M2.5 4h.01M2.5 8h.01M2.5 12h.01",
  activity: "M1.5 8h3l2-5 3 10 2-5h3",
  safety:
    "M8 1.5l5.5 2v4.2c0 3.3-2.3 5.6-5.5 6.8-3.2-1.2-5.5-3.5-5.5-6.8V3.5z M5.5 8l1.8 1.8L10.8 6.3",
  preflight: "M2.5 2.5h11v11h-11z M5 8.2l2 2 4-4.4",
  connections:
    "M6.5 9.5l3-3M5 7L3.6 8.4a2.5 2.5 0 003.5 3.5L8.5 10.5M11 9l1.4-1.4a2.5 2.5 0 00-3.5-3.5L7.5 5.5",
  focus: "M8 1.5a6.5 6.5 0 100 13 6.5 6.5 0 000-13z M8 5.5a2.5 2.5 0 100 5 2.5 2.5 0 000-5z",
  search: "M7 2.5a4.5 4.5 0 100 9 4.5 4.5 0 000-9z M10.3 10.3l3.2 3.2",
}

export interface IconProps {
  name: IconName
  /** The edge length in pixels. */
  size?: number
  className?: string
}

export function Icon({ name, size = 16, className }: IconProps) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.4}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
    >
      <path d={paths[name]} />
    </svg>
  )
}

/** Every icon, for the story that shows them side by side. */
export const iconNames = Object.keys(paths) as IconName[]
