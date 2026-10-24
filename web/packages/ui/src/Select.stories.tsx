import type { Meta, StoryObj } from "@storybook/react-vite"
import { useState } from "react"

import { Select } from "./Select"

function NamespaceChoice() {
  const [value, setValue] = useState("storefront")
  return (
    <div className="flex flex-col gap-1.5 text-sm font-medium">
      <label htmlFor="namespace">Namespace</label>
      <Select
        id="namespace"
        value={value}
        onChange={setValue}
        options={[
          { value: "storefront", label: "storefront" },
          { value: "billing", label: "billing" },
        ]}
      />
    </div>
  )
}

const meta = {
  title: "Components/Select",
  component: NamespaceChoice,
} satisfies Meta<typeof NamespaceChoice>

export default meta
type Story = StoryObj<typeof meta>

export const Labelled: Story = {}
