import type { Meta, StoryObj } from "@storybook/react-vite"

import { Button } from "./Button"

const meta = {
  title: "Components/Button",
  component: Button,
  args: { children: "Approve this plan…", variant: "primary" },
  argTypes: {
    variant: { control: "inline-radio", options: ["primary", "secondary", "attention"] },
  },
} satisfies Meta<typeof Button>

export default meta
type Story = StoryObj<typeof meta>

export const Primary: Story = {}
export const Secondary: Story = { args: { variant: "secondary", children: "Not now" } }
export const Attention: Story = {
  args: { variant: "attention", children: "Acknowledge this run…" },
}
export const Disabled: Story = { args: { disabled: true, children: "Confirm" } }
