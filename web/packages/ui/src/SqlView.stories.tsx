import type { Meta, StoryObj } from "@storybook/react-vite"

import { SqlView } from "./SqlView"

const meta = {
  title: "Components/SqlView",
  component: SqlView,
  args: {
    lines: [
      { number: 1, sql: "ALTER TABLE public.orders ADD COLUMN fulfillment_status text;" },
      {
        number: 2,
        sql: "CREATE INDEX idx_orders_fulfillment ON public.orders (fulfillment_status);",
      },
      {
        number: 3,
        sql: "GRANT SELECT ON public.orders TO reporting_reader;",
        tag: "grants",
        tagTone: "warn",
      },
      {
        number: 4,
        sql: "ALTER TABLE public.orders DROP COLUMN legacy_reference;",
        tag: "drops a column",
        tagTone: "bad",
      },
    ],
  },
} satisfies Meta<typeof SqlView>

export default meta

export const Statements: StoryObj<typeof meta> = {}

// Markup in a statement is text: nothing in the plan is interpreted.
export const MarkupIsText: StoryObj<typeof meta> = {
  args: {
    lines: [
      {
        number: 1,
        sql: "COMMENT ON TABLE t IS '<script>alert(1)</script> \u001b[31mred\u001b[0m';",
      },
    ],
  },
}
