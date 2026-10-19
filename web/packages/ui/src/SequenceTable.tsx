export interface SequenceRow {
  /** The step's version, such as "014". */
  version: string
  /** What the step is, such as its file name or description. */
  name: string
  /** A short checksum or note. */
  note: string
}

/** Ordered steps of a migration: version, name and checksum. */
export function SequenceTable({ rows, caption }: { rows: SequenceRow[]; caption: string }) {
  return (
    <div className="overflow-hidden rounded-xl border border-line2">
      <table className="w-full border-collapse font-mono text-[12.5px]">
        <caption className="sr-only">{caption}</caption>
        <thead className="sr-only">
          <tr>
            <th scope="col">Version</th>
            <th scope="col">Step</th>
            <th scope="col">Checksum</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.version} className="border-b border-line2 last:border-b-0">
              <td className="w-16 px-2.5 py-2 align-top">{row.version}</td>
              <td className="max-w-0 truncate px-2.5 py-2">{row.name}</td>
              <td className="w-24 px-2.5 py-2 text-mute">{row.note}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
