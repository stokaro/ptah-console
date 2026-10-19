import type { Meta, StoryObj } from "@storybook/react-vite"

import { StepRail } from "./StepRail"

const meta = {
  title: "Components/StepRail",
  component: StepRail,
  args: { label: "Progress of storefront", steps: [] },
} satisfies Meta<typeof StepRail>

export default meta
type Story = StoryObj<typeof meta>

export const WaitingForApproval: Story = {
  args: {
    steps: [
      { label: "Resolve", tone: "ok" },
      { label: "Plan", tone: "ok" },
      { label: "Approve", tone: "warn", detail: "Waits for an approval." },
      { label: "Apply", tone: "off" },
      { label: "Verify", tone: "off" },
    ],
  },
}

export const Applying: Story = {
  args: {
    steps: [
      { label: "Resolve", tone: "ok" },
      { label: "Plan", tone: "ok" },
      { label: "Approve", tone: "ok" },
      { label: "Apply", tone: "run" },
      { label: "Verify", tone: "off" },
    ],
  },
}

export const MigrationOutcomeUnknown: Story = {
  args: {
    label: "Progress of billing",
    steps: [
      { label: "Resolve", tone: "ok" },
      { label: "Read history", tone: "ok" },
      { label: "Plan", tone: "ok" },
      { label: "Approve", tone: "ok" },
      { label: "Apply", tone: "unk" },
      { label: "Verify history", tone: "off" },
    ],
  },
}
