import type { Meta, StoryObj } from "@storybook/react-vite"

import { FindingList } from "./FindingList"

const meta = {
  title: "Components/FindingList",
  component: FindingList,
  args: {
    findings: [
      { tone: "bad", text: "Destructive: removes or rewrites data, as reported by the plan" },
      { tone: "unk", text: "Changes privileges: GRANT" },
      { tone: "ok", text: "SQL verified · 3 of 3 chunks · digests match" },
    ],
  },
} satisfies Meta<typeof FindingList>

export default meta

export const Plan: StoryObj<typeof meta> = {}

export const Compact: StoryObj<typeof meta> = { args: { size: "sm" } }
