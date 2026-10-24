import type { MessageInitShape } from "@bufbuild/protobuf"

import type { ResourceDraftSchema } from "../gen/ptah/console/v1/authoring_pb"

/** What a person creates: a PtahSchema or a PtahMigration. */
export type AuthoringKind = "schema" | "migration"

/** Where the desired schema, or the migration sequence, comes from. */
export type SourceTemplate = "oci" | "configmap" | "git"

export const sourceTemplates: { value: SourceTemplate; label: string }[] = [
  { value: "oci", label: "An OCI artifact" },
  { value: "configmap", label: "A ConfigMap" },
  { value: "git", label: "A Git repository" },
]

/** The engines the operator this console is built against supports. */
export const engines = ["PostgreSQL", "MySQL"] as const
export type Engine = (typeof engines)[number]

export const applyPolicies = [
  {
    value: "OnApproval",
    label: "On approval",
    about: "Each plan waits for an approval naming it.",
  },
  {
    value: "Always",
    label: "Always",
    about: "A plan the operator judges safe applies without one.",
  },
  { value: "Never", label: "Never", about: "The operator plans and reports, and applies nothing." },
] as const
export type ApplyPolicy = (typeof applyPolicies)[number]["value"]

/** The form as a person fills it in. */
export interface DraftForm {
  kind: AuthoringKind
  template: SourceTemplate
  namespace: string
  name: string
  engine: Engine
  /** Whether the database is named by a key of this namespace or by a realm. */
  coordination: "key" | "realm"
  coordinationKey: string
  realm: string
  secretName: string
  secretKey: string
  ociRef: string
  policyName: string
  policyKey: string
  configMapName: string
  configMapKey: string
  configMapPath: string
  gitUrl: string
  gitRefKind: "branch" | "tag" | "commit"
  gitRef: string
  gitPath: string
  gitSecret: string
  apply: ApplyPolicy
}

/** A new form, with the defaults the operator's own examples use. */
export function emptyForm(
  kind: AuthoringKind,
  template: SourceTemplate,
  namespace: string
): DraftForm {
  return {
    kind,
    template,
    namespace,
    name: "",
    engine: "PostgreSQL",
    coordination: "key",
    coordinationKey: "",
    realm: "",
    secretName: "",
    secretKey: "url",
    ociRef: "",
    policyName: "",
    policyKey: "policy.yaml",
    configMapName: "",
    configMapKey: "",
    configMapPath: "",
    gitUrl: "",
    gitRefKind: "branch",
    gitRef: "",
    gitPath: "",
    gitSecret: "",
    apply: "OnApproval",
  }
}

/** The draft the console sends: the fields the template uses, and no other. */
export function toDraft(form: DraftForm): MessageInitShape<typeof ResourceDraftSchema> {
  const trim = (value: string) => value.trim()
  const source = (): MessageInitShape<typeof ResourceDraftSchema>["source"] => {
    switch (form.template) {
      case "oci":
        return {
          kind: {
            case: "oci",
            value: {
              ref: trim(form.ociRef),
              verificationPolicyName: trim(form.policyName),
              verificationPolicyKey: trim(form.policyKey),
            },
          },
        }
      case "configmap":
        return {
          kind: {
            case: "configMap",
            value: {
              name: trim(form.configMapName),
              key: trim(form.configMapKey),
              path: trim(form.configMapPath),
            },
          },
        }
      case "git":
        return {
          kind: {
            case: "git",
            value: {
              url: trim(form.gitUrl),
              branch: form.gitRefKind === "branch" ? trim(form.gitRef) : "",
              tag: form.gitRefKind === "tag" ? trim(form.gitRef) : "",
              commit: form.gitRefKind === "commit" ? trim(form.gitRef) : "",
              path: trim(form.gitPath),
              credentialsSecret: trim(form.gitSecret),
            },
          },
        }
    }
  }
  return {
    namespace: form.namespace,
    name: trim(form.name),
    target: {
      engine: form.engine,
      coordinationKey: form.coordination === "key" ? trim(form.coordinationKey) : "",
      realm: form.coordination === "realm" ? trim(form.realm) : "",
      urlSecretName: trim(form.secretName),
      urlSecretKey: trim(form.secretKey),
    },
    source: source(),
    applyPolicy: form.apply,
  }
}

/**
 * The fields the template needs that are still empty, by label. The API
 * server judges everything else; this only keeps a check from being sent
 * that can only be refused for a blank field.
 */
export function missing(form: DraftForm): string[] {
  const required: [string, string][] = [
    ["Namespace", form.namespace],
    ["Name", form.name],
    form.coordination === "key"
      ? ["Coordination key", form.coordinationKey]
      : ["Realm", form.realm],
    ["Connection Secret", form.secretName],
    ["Secret key", form.secretKey],
  ]
  switch (form.template) {
    case "oci":
      required.push(
        ["Artifact", form.ociRef],
        ["Verification policy", form.policyName],
        ["Policy key", form.policyKey]
      )
      break
    case "configmap":
      required.push(
        ["ConfigMap", form.configMapName],
        ["Key", form.configMapKey],
        ["File name", form.configMapPath]
      )
      break
    case "git":
      required.push(["Repository", form.gitUrl], ["Ref", form.gitRef], ["Path", form.gitPath])
      break
  }
  return required.filter(([, value]) => value.trim() === "").map(([label]) => label)
}

/** Two forms that send the same draft have the same key. */
export function draftKey(form: DraftForm) {
  return JSON.stringify({ kind: form.kind, draft: toDraft(form) })
}
