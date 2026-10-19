import type { Meta, StoryObj } from "@storybook/react-vite"

import { Button } from "./Button"
import { ConfirmDialog, DialogDescription } from "./ConfirmDialog"

const meta = {
  title: "Components/ConfirmDialog",
  component: ConfirmDialog,
  args: {
    open: true,
    onOpenChange: () => {},
    where: "kind-ptah / application",
    as: "system:serviceaccount:application:demo-approver",
    title: "Approve storefront-p-8f2c?",
    children: null,
  },
} satisfies Meta<typeof ConfirmDialog>

export default meta
type Story = StoryObj<typeof meta>

export const Confirm: Story = {
  args: {
    children: (
      <>
        <DialogDescription className="m-0 text-[13.5px] text-fg2">
          Sends the exact plan you reviewed (sha256:9c24…7b10). Admission records you as the
          approver.
        </DialogDescription>
        <div className="flex justify-end gap-2">
          <Button>Cancel</Button>
          <Button variant="primary">Confirm</Button>
        </div>
      </>
    ),
  },
}

export const Admitted: Story = {
  args: {
    title: "Approval admitted",
    titleTone: "green",
    children: (
      <DialogDescription className="m-0 text-[13.5px] text-fg2">
        storefront-p-8f2c · recorded by admission as demo-approver. Approved is not yet applied.
      </DialogDescription>
    ),
  },
}

export const Refused: Story = {
  args: {
    title: "Refused",
    titleTone: "red",
    children: (
      <pre className="m-0 rounded-[10px] bg-greybg p-3 font-mono text-xs whitespace-pre-wrap">
        admission webhook &quot;vptahschemaapproval.operator.ptah.run&quot; denied the request: the
        author may not approve
      </pre>
    ),
  },
}
