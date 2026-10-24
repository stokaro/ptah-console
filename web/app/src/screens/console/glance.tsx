import { StatusGlyph, type Tone } from "@ptah/console-ui"

/** One number a reader checks first, with what it counts. */
export interface GlanceTile {
  label: string
  value: string
  sub: string
  tone: Tone
}

/** The tiles at the top of a resource's page. */
export function GlanceTiles({ tiles }: { tiles: GlanceTile[] }) {
  return (
    <ul
      aria-label="At a glance"
      className="m-0 grid list-none grid-cols-2 gap-2.5 p-0 lg:grid-cols-4"
    >
      {tiles.map((tile) => (
        <li
          key={tile.label}
          className="flex flex-col gap-2 rounded-2xl border border-line bg-card p-3.5 shadow-card"
        >
          <span className="flex items-center gap-2 text-[13px] font-medium text-fg2">
            <StatusGlyph tone={tile.tone} size={20} />
            {tile.label}
          </span>
          <span className="font-mono text-[28px] leading-none font-semibold tracking-[-0.03em]">
            {tile.value}
          </span>
          <span className="text-xs text-mute">{tile.sub}</span>
        </li>
      ))}
    </ul>
  )
}
