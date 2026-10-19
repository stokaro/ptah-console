import type { Meta, StoryObj } from "@storybook/react-vite"

import { CommandBlock } from "./CommandBlock"

const meta = {
  title: "Components/CommandBlock",
  component: CommandBlock,
  args: { command: "kubectl ptah migration billing -n application" },
} satisfies Meta<typeof CommandBlock>

export default meta

export const Command: StoryObj<typeof meta> = {}
