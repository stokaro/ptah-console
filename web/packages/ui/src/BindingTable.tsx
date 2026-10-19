export interface Binding {
  name: string
  value: string
}

/** What a decision is bound to: name and value pairs. */
export function BindingTable({ rows }: { rows: Binding[] }) {
  return (
    <dl className="m-0 flex flex-col">
      {rows.map((row) => (
        <div
          key={row.name}
          className="grid grid-cols-[130px_minmax(0,1fr)] gap-2.5 border-t border-line2 py-2 text-[13px]"
        >
          <dt className="text-mute">{row.name}</dt>
          <dd className="m-0 font-mono text-[12.5px] break-all">{row.value}</dd>
        </div>
      ))}
    </dl>
  )
}
