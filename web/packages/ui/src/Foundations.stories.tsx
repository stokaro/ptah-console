import type { Meta, StoryObj } from "@storybook/react-vite"

import { StatusGlyph } from "./StatusGlyph"
import { toneStyle, type Tone } from "./tone"

const tones: Tone[] = ["ok", "warn", "bad", "unk", "run", "off"]
const surfaces = ["bg", "card", "line", "line2", "fg", "fg2", "mute", "pri", "term"]

function Foundations() {
  return (
    <div className="flex flex-col gap-8">
      <section className="flex flex-col gap-3">
        <h2 className="m-0 font-display text-xl font-semibold">States</h2>
        <p className="m-0 text-sm text-fg2">
          A state is always a color and a glyph, never color alone, and its color is its own: the
          accent a viewer chooses changes --pri and nothing a state is drawn with.
        </p>
        <ul className="m-0 grid list-none grid-cols-3 gap-3 p-0">
          {tones.map((tone) => (
            <li
              key={tone}
              className="flex items-center gap-2.5 rounded-xl border border-line bg-card p-3"
            >
              <StatusGlyph tone={tone} size={28} />
              <span className="font-mono text-sm">{tone}</span>
              <span className="text-xs text-mute">{toneStyle(tone).label}</span>
            </li>
          ))}
        </ul>
      </section>
      <section className="flex flex-col gap-3">
        <h2 className="m-0 font-display text-xl font-semibold">Surfaces and text</h2>
        <ul className="m-0 grid list-none grid-cols-3 gap-3 p-0">
          {surfaces.map((name) => (
            <li
              key={name}
              className="flex items-center gap-2.5 rounded-xl border border-line bg-card p-3"
            >
              <span
                className="size-8 rounded-lg border border-line"
                style={{ background: `var(--${name})` }}
              />
              <span className="font-mono text-sm">--{name}</span>
            </li>
          ))}
        </ul>
      </section>
      <section className="flex flex-col gap-2">
        <h2 className="m-0 font-display text-xl font-semibold">Type</h2>
        <span className="font-display text-[28px] font-semibold tracking-[-0.035em]">
          Bricolage Grotesque for headings
        </span>
        <span className="text-[15px]">Geist for text a person reads.</span>
        <span className="font-mono text-[13px]">Geist Mono for names, digests and SQL.</span>
      </section>
    </div>
  )
}

const meta = {
  title: "Foundations",
  component: Foundations,
} satisfies Meta<typeof Foundations>

export default meta

export const Tokens: StoryObj<typeof meta> = {}
