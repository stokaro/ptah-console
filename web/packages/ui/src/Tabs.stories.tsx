import type { Meta, StoryObj } from "@storybook/react-vite"
import { useState } from "react"

import { Card } from "./Card"
import { Tabs } from "./Tabs"

type Panel = "history" | "events" | "configuration"

function Example() {
  const [value, setValue] = useState<Panel>("history")
  return (
    <Tabs
      label="Sections of billing"
      value={value}
      onChange={setValue}
      tabs={[
        { value: "history", label: "History" },
        { value: "events", label: "Events", count: 3 },
        { value: "configuration", label: "Configuration" },
      ]}
    >
      <Card title={value}>
        <span className="text-sm">The {value} panel.</span>
      </Card>
    </Tabs>
  )
}

const meta = {
  title: "Components/Tabs",
  component: Example,
} satisfies Meta<typeof Example>

export default meta

export const ThreePanels: StoryObj<typeof meta> = {}
