import { Card, StatusGlyph } from "@ptah/console-ui"

import type { DriftFinding, Target } from "../../gen/ptah/console/v1/resources_pb"
import { differenceTone, driftPhrase, driftReading, readingTone } from "../../model/drift"
import {
  differs,
  inventoryRows,
  inventoryTotals,
  otherFindings,
  profileRows,
  radarCorners,
  type KindRow,
} from "../../model/inventory"

function objects(count: number) {
  return `${count} ${count === 1 ? "object" : "objects"}`
}

/** The kinds one side holds, with how many of each. */
function CountList({
  label,
  rows,
  side,
}: {
  label: string
  rows: KindRow[]
  side: "desired" | "observed"
}) {
  const held = rows.filter((row) => row[side] > 0)
  if (held.length === 0) {
    return <span className="text-sm text-fg2">None.</span>
  }
  return (
    <ul
      aria-label={label}
      className="m-0 grid list-none grid-cols-2 gap-x-4 gap-y-1 p-0 text-[13px]"
    >
      {held.map((row) => (
        <li
          key={row.kind}
          className="flex items-baseline justify-between gap-2 border-b border-line py-1"
        >
          <span className="text-fg2">{row.label}</span>
          <span className="font-mono font-semibold">{row[side]}</span>
        </li>
      ))}
    </ul>
  )
}

// The declared side is an outline in a fixed neutral and the database's side
// a solid teal, so the two differ in shape as well as color, and neither
// follows the accent a viewer picks for actions.
const sides = [
  { key: "desired", label: "Declared in the schema", mark: "border border-fg2" },
  { key: "observed", label: "Held by the database", mark: "bg-teal" },
] as const

/**
 * Each kind's two counts as a pair of bars on one scale. The bars are drawn
 * for the eye; the table beside them says the same to a screen reader.
 */
function ComparisonBars({ rows, name }: { rows: KindRow[]; name: string }) {
  const most = Math.max(1, ...rows.map((row) => Math.max(row.desired, row.observed)))
  return (
    <>
      <ul aria-hidden="true" className="m-0 flex list-none flex-wrap gap-3 p-0 text-xs text-fg2">
        {sides.map((side) => (
          <li key={side.key} className="inline-flex items-center gap-1.5">
            <span aria-hidden="true" className={`size-2.5 rounded-sm ${side.mark}`} />
            {side.label}
          </li>
        ))}
      </ul>
      <div aria-hidden="true" className="flex flex-col gap-2">
        {rows.map((row) => (
          <div
            key={row.kind}
            className="group grid grid-cols-[minmax(88px,auto)_minmax(0,1fr)] items-center gap-2 rounded-md px-1 py-0.5 text-xs hover:bg-hover"
          >
            <span className="text-fg2 group-hover:text-fg">{row.label}</span>
            <span className="flex flex-col gap-0.5">
              {sides.map((side) => (
                <span key={side.key} className="flex items-center gap-1.5">
                  <span className="h-2 flex-auto">
                    {row[side.key] > 0 ? (
                      <span
                        className={`block h-2 rounded-full ${side.mark}`}
                        style={{ width: `${(row[side.key] / most) * 100}%`, minWidth: 8 }}
                      />
                    ) : null}
                  </span>
                  <span className="w-9 text-right font-mono text-[11px]">{row[side.key]}</span>
                </span>
              ))}
            </span>
          </div>
        ))}
      </div>
      <table className="sr-only">
        <caption>Declared and held objects of {name}</caption>
        <thead>
          <tr>
            <th scope="col">Kind</th>
            <th scope="col">Declared in the schema</th>
            <th scope="col">Held by the database</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.kind}>
              <th scope="row">{row.label}</th>
              <td>{row.desired}</td>
              <td>{row.observed}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}

/**
 * What a kind that differs has: only declared, only in the database, changed
 * on both, and what the findings about its objects say.
 */
function kindParts(row: KindRow) {
  if (row.matched === undefined) {
    return ["its counts and its findings disagree"]
  }
  return [
    row.onlyDesired > 0 ? `${row.onlyDesired} only declared` : "",
    row.onlyObserved > 0 ? `${row.onlyObserved} only in the database` : "",
    row.modified > 0 ? `${row.modified} changed` : "",
    ...row.notes.map((note) => `${note.count} ${note.phrase}`),
    row.severity,
  ].filter(Boolean)
}

/**
 * What the last read found, kind by kind, and the findings that belong to no
 * kind by their category. A kind with no finding is said to have none, not to
 * match: a table whose columns changed has no finding of its own and still
 * differs from its declaration.
 */
function DriftList({ rows, others }: { rows: KindRow[]; others: DriftFinding[] }) {
  const differing = rows.filter(differs)
  const quiet = rows.length - differing.length
  return (
    <>
      {differing.length > 0 ? (
        <ul
          aria-label="Kinds that differ"
          className="m-0 flex list-none flex-col gap-1.5 p-0 text-[13px]"
        >
          {differing.map((row) => (
            <li key={row.kind} className="flex items-start gap-2 border-b border-line py-1">
              <StatusGlyph
                tone={row.matched === undefined ? "unk" : differenceTone(row.severity)}
                size={20}
              />
              <span className="flex flex-col">
                <span className="font-medium">{row.label}</span>
                <span className="text-xs text-fg2">{kindParts(row).join(" · ")}</span>
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {others.length > 0 ? (
        <ul
          aria-label="Other findings"
          className="m-0 flex list-none flex-col gap-1.5 p-0 text-[13px]"
        >
          {others.map((finding) => (
            <li key={finding.category} className="flex items-start gap-2 border-b border-line py-1">
              <StatusGlyph tone={differenceTone(finding.severity)} size={20} />
              <span className="flex flex-col">
                <span className="font-mono text-xs font-medium break-all">{finding.category}</span>
                <span className="text-xs text-fg2">
                  {finding.count} · {finding.severity}
                </span>
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {quiet > 0 && (differing.length > 0 || others.length > 0) ? (
        <span className="text-[13px] text-fg2">
          {differing.length === 0
            ? "No counted kind has a finding."
            : quiet === 1
              ? "The other kind has no finding."
              : `The other ${quiet} kinds have no finding.`}
        </span>
      ) : null}
    </>
  )
}

/** The sentence over the drift list, from what the operator says the read found. */
function DriftHeadline({ target, listed }: { target?: Target; listed: boolean }) {
  const reading = driftReading(target)
  if (reading.state === "none") {
    // A reading with no severity and a listed finding contradicts itself; the
    // list then says what the operator published, and no sentence covers it.
    return listed ? null : (
      <span className="flex items-center gap-2 text-[13px] text-fg2">
        <StatusGlyph tone="ok" size={20} />
        No object differs between the schema and the database.
      </span>
    )
  }
  if (reading.state === "unread") {
    return <span className="text-[13px] text-fg2">The database is not read yet.</span>
  }
  return (
    <span className="flex items-start gap-2 text-[13px]">
      <StatusGlyph tone={readingTone(reading)} size={20} />
      <span>The last read found {driftPhrase(reading)}.</span>
    </span>
  )
}

// The drawing is wider than tall, so the labels beside the left and right
// axes fit inside it: the longest, such as "Materialized views", run about
// ninety units past the end of their axis.
const radarWidth = 440
const radarHeight = 260
const radarRadius = 86
const cx = radarWidth / 2
const cy = radarHeight / 2

// A radar needs three axes to be a shape, and past eight its labels crowd
// each other; outside that range the coverage is a table.
const radarAxes = { least: 3, most: 8 }

function polygon(values: number[]) {
  return radarCorners(values)
    .map((corner) => `${cx + corner.x * radarRadius},${cy + corner.y * radarRadius}`)
    .join(" ")
}

/**
 * How many of the objects each kind declares the database holds, as a radar:
 * the declared outline is full on every axis, and the database's reaches as
 * far as the coverage. The table says the same as the drawing, and stands in
 * for it when the kinds make no readable radar.
 */
function CoverageRadar({ rows, name }: { rows: KindRow[]; name: string }) {
  const profile = profileRows(rows)
  if (profile.length === 0) {
    return (
      <span className="text-sm text-fg2">The schema declares nothing whose coverage is known.</span>
    )
  }
  const drawn = profile.length >= radarAxes.least && profile.length <= radarAxes.most
  const table = (
    <table className={drawn ? "sr-only" : "w-full text-left text-[13px]"}>
      <caption className="sr-only">Coverage per kind of {name}</caption>
      <thead className={drawn ? undefined : "sr-only"}>
        <tr>
          <th scope="col">Kind</th>
          <th scope="col">Held of declared</th>
          <th scope="col">Coverage</th>
        </tr>
      </thead>
      <tbody>
        {profile.map(({ row, percent }) => (
          <tr key={row.kind} className="border-b border-line last:border-b-0">
            <th scope="row" className="py-1 font-medium">
              {row.label}
            </th>
            <td className="py-1 text-fg2">
              {row.matched} of {row.desired}
            </td>
            <td className="py-1 text-right font-mono">{percent}%</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
  if (!drawn) {
    return table
  }
  const labels = radarCorners(profile.map(() => 1)).map((corner) => ({
    x: cx + corner.x * (radarRadius + 12),
    y: cy + corner.y * (radarRadius + 12),
    anchor: (Math.abs(corner.x) < 0.2 ? "middle" : corner.x > 0 ? "start" : "end") as
      "middle" | "start" | "end",
    // A label above the drawing grows upward, one below it downward.
    lift: corner.y < -0.5 ? -1 : corner.y > 0.5 ? 1 : 0,
  }))
  return (
    <>
      <svg
        aria-hidden="true"
        viewBox={`0 0 ${radarWidth} ${radarHeight}`}
        className="mx-auto w-full max-w-[480px]"
      >
        {[0.25, 0.5, 0.75, 1].map((ring) => (
          <polygon
            key={ring}
            points={polygon(profile.map(() => ring))}
            className="fill-none stroke-line"
          />
        ))}
        {radarCorners(profile.map(() => 1)).map((corner, index) => (
          <line
            key={profile[index].row.kind}
            x1={cx}
            y1={cy}
            x2={cx + corner.x * radarRadius}
            y2={cy + corner.y * radarRadius}
            className="stroke-line"
          />
        ))}
        <polygon
          points={polygon(profile.map(() => 1))}
          className="fill-none stroke-fg2"
          strokeWidth={1.5}
        />
        <polygon
          points={polygon(profile.map(({ share }) => share))}
          className="fill-[color-mix(in_oklab,var(--teal)_28%,transparent)] stroke-teal"
          strokeWidth={2}
        />
        {labels.map((label, index) => (
          <text
            key={profile[index].row.kind}
            x={label.x}
            y={label.y + (label.lift === -1 ? -12 : label.lift === 1 ? 4 : -4)}
            textAnchor={label.anchor}
            className="fill-fg2 text-[10px]"
          >
            <tspan x={label.x} dy="0.35em">
              {profile[index].row.label}
            </tspan>
            <tspan x={label.x} dy="1.15em" className="fill-fg font-mono font-semibold">
              {profile[index].percent}%
            </tspan>
          </text>
        ))}
      </svg>
      <ul aria-hidden="true" className="m-0 flex list-none flex-wrap gap-3 p-0 text-xs text-fg2">
        <li className="inline-flex items-center gap-1.5">
          <span aria-hidden="true" className="size-2.5 rounded-sm border border-fg2" />
          Declared, every one
        </li>
        <li className="inline-flex items-center gap-1.5">
          <span aria-hidden="true" className="size-2.5 rounded-sm bg-teal" />
          Held by the database
        </li>
      </ul>
      {table}
    </>
  )
}

/**
 * What the desired schema declares and what the database holds, by kind of
 * object, as the operator's last comparison counted them: each side's counts,
 * the two side by side, what differs, and how much of each kind the database
 * holds. The counts name no object.
 */
export function InventoryCards({ target, name }: { target?: Target; name: string }) {
  const rows = inventoryRows(target)
  if (rows.length === 0) {
    return (
      <Card title="Desired and observed state">
        <span className="text-sm text-fg2">
          {target?.lastObservedAt
            ? "The last read published no object counts. Either neither side holds an object the comparison counts, or the operator or its executor does not count them: counts need operator 0.2.8 or later with Ptah v0.16.0 or later."
            : "The database is not read yet, so no object is counted."}
        </span>
      </Card>
    )
  }
  const totals = inventoryTotals(rows)
  const others = otherFindings(target)
  return (
    <>
      <div className="grid gap-4 lg:grid-cols-3">
        <Card title="Desired state">
          <span className="text-xs text-mute">{objects(totals.desired)} counted in the schema</span>
          <CountList label="Objects declared in the schema" rows={rows} side="desired" />
        </Card>
        <Card title="Observed state">
          <span className="text-xs text-mute">
            {objects(totals.observed)} counted in the database, in the compared scope
          </span>
          <CountList label="Objects found in the database" rows={rows} side="observed" />
        </Card>
        <Card title="Schema comparison">
          <span className="text-xs text-mute">Declared and held objects, by kind</span>
          <ComparisonBars rows={rows} name={name} />
          <span className="text-xs text-mute">
            Counted by the operator&apos;s last comparison. Columns and enum values are counted only
            in the tables and enum types both sides hold. An object the comparison withheld as
            undecided is counted on neither side, and objects a feature owner compares, such as
            PostgreSQL extensions, are not counted.
          </span>
        </Card>
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card title="Object drift">
          <DriftHeadline target={target} listed={rows.some(differs) || others.length > 0} />
          <DriftList rows={rows} others={others} />
        </Card>
        <Card title="Schema profile">
          <span className="text-xs text-mute">
            How many of the objects each kind declares the database holds, changed or not. Object
            drift says what changed.
          </span>
          <CoverageRadar rows={rows} name={name} />
        </Card>
      </div>
    </>
  )
}
