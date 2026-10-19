import type { Meta, StoryObj } from "@storybook/react-vite"

import { Wordmark } from "./Wordmark"

const meta = {
  title: "Components/Wordmark",
  component: Wordmark,
} satisfies Meta<typeof Wordmark>

export default meta

export const Focus: StoryObj<typeof meta> = {}
