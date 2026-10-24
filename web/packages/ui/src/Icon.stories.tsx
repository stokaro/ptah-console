import type { Meta, StoryObj } from "@storybook/react-vite"

import { Icon, iconNames } from "./Icon"

const meta = {
  title: "Components/Icon",
  component: Icon,
  args: { name: "overview" },
} satisfies Meta<typeof Icon>

export default meta

export const One: StoryObj<typeof meta> = {}

export const Every: StoryObj<typeof meta> = {
  render: () => (
    <ul className="m-0 grid list-none grid-cols-4 gap-4 p-0 text-sm text-fg2">
      {iconNames.map((name) => (
        <li key={name} className="flex items-center gap-2">
          <Icon name={name} />
          {name}
        </li>
      ))}
    </ul>
  ),
}
