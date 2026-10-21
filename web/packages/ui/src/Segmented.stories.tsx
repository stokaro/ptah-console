import type { Meta, StoryObj } from "@storybook/react-vite"
import { useState } from "react"

import { Segmented } from "./Segmented"

function KindFilter() {
  const [value, setValue] = useState<"all" | "schema" | "migration">("all")
  return (
    <Segmented
      label="Kind"
      value={value}
      onChange={setValue}
      options={[
        { value: "all", label: "All", count: 12 },
        { value: "schema", label: "Schemas", count: 9 },
        { value: "migration", label: "Migrations", count: 3 },
      ]}
    />
  )
}

const meta = {
  title: "Components/Segmented",
  component: KindFilter,
} satisfies Meta<typeof KindFilter>

export default meta
type Story = StoryObj<typeof meta>

export const WithCounts: Story = {}
