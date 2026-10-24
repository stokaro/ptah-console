import type { Meta, StoryObj } from "@storybook/react-vite"

import { ScrollRegion } from "./ScrollRegion"

const meta = {
  title: "Components/ScrollRegion",
  component: ScrollRegion,
  args: {
    label: "Phases of storefront",
    className: "max-w-sm",
    children: (
      <table className="min-w-[640px] text-left text-[13px]">
        <caption className="sr-only">Phases of storefront</caption>
        <thead>
          <tr>
            {["Resolve", "Plan", "Approve", "Apply", "Verify"].map((phase) => (
              <th key={phase} scope="col" className="px-3 py-2">
                {phase}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          <tr>
            {["done", "done", "waiting", "–", "–"].map((state, index) => (
              <td key={index} className="px-3 py-2">
                {state}
              </td>
            ))}
          </tr>
        </tbody>
      </table>
    ),
  },
} satisfies Meta<typeof ScrollRegion>

export default meta

export const WideTable: StoryObj<typeof meta> = {}
