import type { Meta, StoryObj } from "@storybook/react-vite"

import { Card } from "./Card"

const meta = {
  title: "Components/Card",
  component: Card,
  args: { title: "What you can do", children: <span className="text-sm">Nothing to do.</span> },
} satisfies Meta<typeof Card>

export default meta

export const Section: StoryObj<typeof meta> = {}
