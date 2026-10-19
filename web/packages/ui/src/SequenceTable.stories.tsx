import type { Meta, StoryObj } from "@storybook/react-vite"

import { SequenceTable } from "./SequenceTable"

const meta = {
  title: "Components/SequenceTable",
  component: SequenceTable,
  args: {
    caption: "Steps in order",
    rows: [
      { version: "014", name: "add_ledger_partitions.sql", note: "3e1a9c2" },
      { version: "015", name: "add_invoice_currency.sql", note: "b0f4d17" },
      { version: "016", name: "grant_reporting_ledger.sql", note: "7c22e90" },
      { version: "017", name: "drop_legacy_totals.sql", note: "e51a0bd" },
    ],
  },
} satisfies Meta<typeof SequenceTable>

export default meta

export const MigrationSteps: StoryObj<typeof meta> = {}
