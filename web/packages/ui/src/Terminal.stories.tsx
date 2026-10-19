import type { Meta, StoryObj } from "@storybook/react-vite"

import { Terminal } from "./Terminal"

const meta = {
  title: "Components/Terminal",
  component: Terminal,
  args: {
    label: "Log excerpt",
    text: 'applying 014_add_ledger_partitions.sql\napplying 015_add_invoice_currency.sql\nERROR: relation "ledger" does not exist\n',
  },
} satisfies Meta<typeof Terminal>

export default meta

export const LogExcerpt: StoryObj<typeof meta> = {}
