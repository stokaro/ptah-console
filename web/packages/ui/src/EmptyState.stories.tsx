import type { Meta, StoryObj } from "@storybook/react-vite"

import { EmptyState } from "./EmptyState"

const meta = {
  title: "Components/EmptyState",
  component: EmptyState,
  args: { title: "Nothing waits for you", children: "Approved plans continue in the operator." },
} satisfies Meta<typeof EmptyState>

export default meta

export const NothingWaits: StoryObj<typeof meta> = {}

export const NotSignedIn: StoryObj<typeof meta> = {
  args: {
    tone: "warn",
    title: "This page is not signed in",
    children: "Open the link ptah-console printed in your terminal.",
  },
}
