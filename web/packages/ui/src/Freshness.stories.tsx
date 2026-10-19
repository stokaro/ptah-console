import type { Meta, StoryObj } from "@storybook/react-vite"

import { Freshness } from "./Freshness"

const now = new Date("2026-10-20T10:00:30Z")

const meta = {
  title: "Components/Freshness",
  component: Freshness,
  args: { state: "live", at: new Date("2026-10-20T10:00:18Z"), now },
} satisfies Meta<typeof Freshness>

export default meta
type Story = StoryObj<typeof meta>

export const Live: Story = {}
export const Lost: Story = { args: { state: "lost", at: new Date("2026-10-20T09:58:00Z") } }
export const Denied: Story = { args: { state: "denied", at: undefined } }
