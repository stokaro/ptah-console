import type { Meta, StoryObj } from "@storybook/react-vite"

import { StatusGlyph } from "./StatusGlyph"

const meta = {
  title: "Components/StatusGlyph",
  component: StatusGlyph,
  args: { tone: "ok", size: 24 },
  argTypes: {
    tone: { control: "inline-radio", options: ["ok", "warn", "bad", "unk", "run", "off"] },
    size: { control: "inline-radio", options: [20, 22, 24, 28] },
  },
} satisfies Meta<typeof StatusGlyph>

export default meta
type Story = StoryObj<typeof meta>

export const Decorative: Story = {}

export const Labelled: Story = { args: { tone: "bad", label: "failed" } }
