import type { Meta, StoryObj } from "@storybook/react-vite"

import { BindingTable } from "./BindingTable"

const meta = {
  title: "Components/BindingTable",
  component: BindingTable,
  args: {
    rows: [
      { name: "Artifact", value: "sha256:5f2c0d1e…a1b2e7ae" },
      { name: "Computed at", value: "10/19/2026, 11:12:51 PM" },
      { name: "Policy", value: "apply: OnApproval" },
      { name: "Plan contract", value: "1" },
    ],
  },
} satisfies Meta<typeof BindingTable>

export default meta

export const BoundTo: StoryObj<typeof meta> = {}
