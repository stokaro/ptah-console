export interface ToastProps {
  title: string
  detail?: string
}

/** A short message at the bottom of the page that something happened. */
export function Toast({ title, detail }: ToastProps) {
  return (
    <div
      role="status"
      aria-live="polite"
      className="fixed bottom-5 left-1/2 z-70 box-border flex w-[min(420px,calc(100vw-24px))] -translate-x-1/2 flex-col gap-0.5 rounded-[18px] border border-l-[3px] border-line border-l-pri bg-card px-3.5 py-3 shadow-card"
    >
      <span className="text-[13.5px] font-semibold">{title}</span>
      {detail ? <span className="text-[12.5px] break-words text-mute">{detail}</span> : null}
    </div>
  )
}
