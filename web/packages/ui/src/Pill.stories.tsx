import type { Meta, StoryObj } from "@storybook/react-vite"

import { Pill } from "./Pill"

const meta = {
  title: "Components/Pill",
  component: Pill,
  args: { children: "Read-only" },
} satisfies Meta<typeof Pill>

export default meta
type Story = StoryObj<typeof meta>

export const Plain: Story = {}
export const Warning: Story = { args: { tone: "warn", children: "Status is stale" } }
