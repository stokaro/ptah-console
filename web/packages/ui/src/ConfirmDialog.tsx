import { AlertDialog } from "@base-ui/react/alert-dialog"
import type { ReactNode } from "react"

export interface ConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The heading: what is being decided, or how it ended. */
  title: string
  titleTone?: "fg" | "green" | "amber" | "red" | "violet"
  /** Where the decision lands, such as a context and a namespace. */
  where: string
  /** Whom the decision is sent as. */
  as: string
  /** While true the dialog cannot be dismissed: a request is on its way. */
  busy?: boolean
  children: ReactNode
}

const titleTones = {
  fg: "text-fg",
  green: "text-green",
  amber: "text-amber",
  red: "text-red",
  violet: "text-violet",
}

/**
 * The dialog every decision goes through. It always says where the decision
 * lands and whom it is sent as, before anything else.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  titleTone = "fg",
  where,
  as,
  busy = false,
  children,
}: ConfirmDialogProps) {
  return (
    <AlertDialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!busy || next) {
          onOpenChange(next)
        }
      }}
    >
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 z-50 bg-[color-mix(in_oklab,var(--bg)_30%,rgba(0,0,0,.5))] backdrop-blur-[6px]" />
        <AlertDialog.Viewport className="fixed inset-0 z-60 grid place-items-end justify-center p-3 sm:place-items-center">
          <AlertDialog.Popup className="flex max-h-[calc(100vh-24px)] w-[min(520px,100%)] animate-[pt-pop_.25s_both] flex-col gap-3.5 overflow-auto rounded-[22px] border border-line bg-card p-[22px] text-fg shadow-card">
            <div className="flex flex-wrap gap-1.5 text-xs">
              <span className="rounded-[9px] bg-greybg px-2.5 py-[5px]">
                <span className="text-mute">Where </span>
                <b className="font-mono font-semibold">{where}</b>
              </span>
              <span className="rounded-[9px] bg-greybg px-2.5 py-[5px]">
                <span className="text-mute">As </span>
                <b className="font-mono font-semibold">{as}</b>
              </span>
            </div>
            <AlertDialog.Title
              className={`m-0 font-display text-xl font-semibold break-words ${titleTones[titleTone]}`}
            >
              {title}
            </AlertDialog.Title>
            {children}
          </AlertDialog.Popup>
        </AlertDialog.Viewport>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  )
}

/** The text under a dialog's title. */
export const DialogDescription = AlertDialog.Description
