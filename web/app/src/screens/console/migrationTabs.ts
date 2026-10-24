/** The panels of a migration's page. History is the one an address without a tab opens. */
export type MigrationTab = "history" | "events" | "configuration" | "related"

export const migrationTabs: readonly MigrationTab[] = [
  "history",
  "events",
  "configuration",
  "related",
]
