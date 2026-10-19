import type { Meta, StoryObj } from "@storybook/react-vite"

import { Toast } from "./Toast"

const meta = {
  title: "Components/Toast",
  component: Toast,
  args: {
    title: "Link copied",
    detail: "http://127.0.0.1:52144/approve/application/schema/storefront",
  },
} satisfies Meta<typeof Toast>

export default meta

export const Copied: StoryObj<typeof meta> = {}
