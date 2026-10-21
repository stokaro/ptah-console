import type { Meta, StoryObj } from "@storybook/react-vite"

import { StepDots } from "./StepDots"

const meta = {
  title: "Components/StepDots",
  component: StepDots,
  args: {
    label: "Lifecycle of storefront",
    steps: [
      { label: "Resolve", tone: "ok" },
      { label: "Plan", tone: "ok" },
      { label: "Approve", tone: "warn" },
      { label: "Apply", tone: "off" },
      { label: "Verify", tone: "off" },
    ],
  },
} satisfies Meta<typeof StepDots>

export default meta
type Story = StoryObj<typeof meta>

export const WaitingForApproval: Story = {}
export const Failed: Story = {
  args: {
    label: "Lifecycle of billing",
    steps: [
      { label: "Resolve", tone: "ok" },
      { label: "Read history", tone: "ok" },
      { label: "Plan", tone: "ok" },
      { label: "Approve", tone: "ok" },
      { label: "Apply", tone: "bad" },
      { label: "Verify history", tone: "off" },
    ],
  },
}
