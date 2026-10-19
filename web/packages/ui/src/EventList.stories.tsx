import type { Meta, StoryObj } from "@storybook/react-vite"

import { EventList } from "./EventList"

const meta = {
  title: "Components/EventList",
  component: EventList,
  args: {
    label: "What happened",
    events: [
      {
        key: "1",
        time: "12:28",
        tone: "ok",
        title: "PlanReady · Published",
        detail: "3 statements",
      },
      {
        key: "2",
        time: "12:28",
        tone: "warn",
        title: "ApprovalRequired · AwaitingApproval",
        detail: "approve storefront-p-8f2c",
      },
      {
        key: "3",
        time: "12:31",
        tone: "run",
        title: "Applying · JobRunning",
        detail: "Job storefront-apply-1",
      },
      {
        key: "4",
        time: "–",
        tone: "off",
        title: "Job billing-run-1: not retained",
        detail: "Kubernetes no longer keeps the Job.",
      },
    ],
  },
} satisfies Meta<typeof EventList>

export default meta

export const Timeline: StoryObj<typeof meta> = {}
