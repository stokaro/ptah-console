import { Button, EmptyState, FindingList } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"
import { useState } from "react"

import { useOffer, useResources, useSession } from "../api/resources"
import {
  age,
  planRisks,
  readingAgain,
  waitingForApproval,
  type ResourceView,
} from "../model/resource"
import {
  Hero,
  Loading,
  ReadError,
  TruncatedNotice,
  UnreadableNamespaces,
  useNow,
  where,
} from "./common"
import { ApproveDialog } from "./ApproveDialog"

/** What a plan does, in a few words, from what the plan reports. */
export function planSummary(resource: ResourceView) {
  const plan = resource.plan
  if (resource.kind === "migration") {
    return "Runs the pending migrations in order"
  }
  if (!plan) {
    return "The operator is planning again; the plan can be approved once it is published"
  }
  const parts: string[] = []
  if (plan.destructive) {
    parts.push("removes or rewrites data")
  }
  if (plan.privilegeChanges.length > 0) {
    parts.push("changes privileges")
  }
  const statements = `${plan.statementCount} ${plan.statementCount === 1 ? "statement" : "statements"}`
  if (parts.length === 0) {
    return `${statements}, none destructive`
  }
  const text = parts.join(" and ")
  return `${statements}; ${text}`
}

export function ApproverQueue() {
  const { resources, scope, truncated, loading, error } = useResources()
  const session = useSession()
  const now = useNow()
  const [approvingUid, setApprovingUid] = useState<string>()
  // The dialog follows the resource as it is read again, so it knows when
  // the operator starts reading it and admission would refuse an approval.
  const approving = resources.find((resource) => resource.ref.uid === approvingUid)
  const context = session.data?.context
  const waiting = resources
    .filter((resource) => waitingForApproval(resource))
    .sort(
      (a, b) => Number(a.plan?.createdAt?.seconds ?? 0n) - Number(b.plan?.createdAt?.seconds ?? 0n)
    )
  const offer = useOffer()
  const approvable = (resource: ResourceView) =>
    offer(
      resource.namespace,
      resource.kind === "schema" ? "approveSchemaPlans" : "approveMigrationPlans"
    ) === "allowed"
  const anywhere = [
    offer(undefined, "approveSchemaPlans"),
    offer(undefined, "approveMigrationPlans"),
  ]

  return (
    <>
      <Hero
        eyebrow="Approver"
        count={waiting.length}
        label={waiting.length === 1 ? "plan waits for a decision" : "plans wait for a decision"}
        sub="Plans waiting for your decision"
        capability={
          anywhere.includes("read-only")
            ? "nothing: this console runs read-only"
            : anywhere.includes("allowed")
              ? "create ptahschemaapprovals, ptahmigrationapprovals"
              : "no approval verb in these namespaces"
        }
      />
      <UnreadableNamespaces scopes={scope.scopes} />
      <TruncatedNotice namespaces={truncated} />
      {loading ? (
        <Loading what="the plans" />
      ) : error ? (
        <ReadError error={error} what="the plans" />
      ) : waiting.length === 0 ? (
        <EmptyState title="Nothing waits for you">
          Approved plans continue in the operator.
        </EmptyState>
      ) : (
        <ul
          className="m-0 flex list-none flex-col gap-3 p-0"
          aria-label="Plans waiting for approval"
        >
          {waiting.map((resource) => {
            const link = {
              to: "/approve/$namespace/$kind/$name",
              params: { namespace: resource.namespace, kind: resource.kind, name: resource.name },
            } as const
            return (
              <li
                key={resource.ref.uid}
                className="flex animate-[pt-rise_.4s_both] flex-col gap-3 rounded-[18px] border border-line bg-card p-[18px] shadow-card transition-[border-color,transform] duration-200 hover:-translate-y-px hover:border-[color-mix(in_oklab,var(--pri)_40%,var(--line))]"
              >
                <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3">
                  <span className="flex min-w-0 flex-col gap-1">
                    <Link {...link} className="font-mono text-base font-semibold break-all text-fg">
                      {resource.plan?.name ?? resource.name}
                    </Link>
                    <span className="text-[13.5px] text-fg2">{planSummary(resource)}</span>
                    <span className="text-xs text-mute">
                      {where(context, resource.namespace)} ·{" "}
                      {resource.kind === "schema" ? "Schema plan" : "Migration plan"} for{" "}
                      {resource.name} · waiting {age(resource.plan?.createdAt, now)}
                    </span>
                  </span>
                  <span aria-hidden="true" className="font-mono text-base font-semibold text-mute">
                    ›
                  </span>
                </div>
                <FindingList
                  size="sm"
                  findings={planRisks(resource).filter((risk) => risk.tone !== "off")}
                />
                <div className="flex flex-wrap gap-2">
                  {resource.plan && approvable(resource) && resource.approvalOpen ? (
                    <Button variant="primary" onClick={() => setApprovingUid(resource.ref.uid)}>
                      Approve…
                    </Button>
                  ) : null}
                  <Link
                    {...link}
                    className="inline-flex min-h-11 items-center justify-center rounded-xl border border-line bg-card px-4 text-sm font-medium text-fg"
                  >
                    Review
                  </Link>
                </div>
                {resource.plan && approvable(resource) && !resource.approvalOpen ? (
                  <span role="status" className="text-[13px] text-fg2">
                    {readingAgain(resource)}
                  </span>
                ) : null}
              </li>
            )
          })}
        </ul>
      )}
      {approving ? (
        <ApproveDialog
          resource={approving}
          context={context}
          onClose={() => setApprovingUid(undefined)}
        />
      ) : null}
    </>
  )
}
