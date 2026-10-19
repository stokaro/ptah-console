import { Button, EmptyState, StatusGlyph } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"

import { canAnywhere, useDecisions, useSession } from "../api/resources"
import { clock } from "../model/resource"
import {
  byNewest,
  decisionNote,
  decisionSegment,
  decisionsCsv,
  decisionState,
  decisionWhat,
} from "../model/decision"
import { Hero, Loading, ReadError, UnreadableNamespaces, where } from "./common"

function download(text: string, filename: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/csv;charset=utf-8" }))
  const anchor = document.createElement("a")
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}

export function Decisions() {
  const { scope, decisions } = useDecisions()
  const session = useSession()
  const list = [...(decisions.data?.decisions ?? [])].sort(byNewest)

  return (
    <>
      <Hero
        eyebrow="Auditor"
        count={list.length}
        label={list.length === 1 ? "decision on record" : "decisions on record"}
        sub="Who decided what, and when"
        capability={
          canAnywhere(scope.scopes, "listDecisions")
            ? "list ptahschemaapprovals, ptahmigrationapprovals"
            : "no list verb on approvals"
        }
      />
      <UnreadableNamespaces scopes={scope.scopes} />
      {scope.isPending || (scope.readable.length > 0 && decisions.isPending) ? (
        <Loading what="the decisions" />
      ) : decisions.error ? (
        <ReadError error={decisions.error} what="the decisions" />
      ) : list.length === 0 ? (
        <EmptyState tone="off" title="No decision on record">
          Approvals and acknowledgments appear here once someone makes one.
        </EmptyState>
      ) : (
        <div className="flex flex-col gap-2.5">
          <Button
            className="self-end"
            onClick={() => download(decisionsCsv(list), "ptah-decisions.csv")}
          >
            Export CSV
          </Button>
          <ul
            aria-label="Decisions"
            className="m-0 flex list-none flex-col overflow-hidden rounded-[18px] border border-line bg-card p-0 shadow-card"
          >
            {list.map((decision) => {
              const state = decisionState(decision.state)
              return (
                <li
                  key={decision.ref?.uid}
                  className="grid grid-cols-[26px_minmax(0,1fr)_auto] items-center gap-3 border-b border-line2 px-4 py-3.5 last:border-b-0 hover:bg-hover"
                >
                  <StatusGlyph tone={state.tone} size={24} label={state.label} />
                  <span className="flex min-w-0 flex-col gap-[3px]">
                    <Link
                      to="/decisions/$namespace/$kind/$name"
                      params={{
                        namespace: decision.ref?.namespace ?? "",
                        kind: decisionSegment(decision.kind),
                        name: decision.ref?.name ?? "",
                      }}
                      className="text-sm font-semibold break-words text-fg"
                    >
                      {decisionWhat(decision)}
                    </Link>
                    <span className="text-[12.5px] text-fg2">
                      {decision.decidedBy} ·{" "}
                      {where(session.data?.context, decision.ref?.namespace ?? "")} ·{" "}
                      {clock(decision.decidedAt)}
                    </span>
                    <span className="font-mono text-xs text-mute">{decisionNote(decision)}</span>
                  </span>
                  <span aria-hidden="true" className="font-mono text-base font-semibold text-mute">
                    ›
                  </span>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </>
  )
}
