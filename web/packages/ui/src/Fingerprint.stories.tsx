import type { Meta, StoryObj } from "@storybook/react-vite"

import { Fingerprint } from "./Fingerprint"

const meta = {
  title: "Components/Fingerprint",
  component: Fingerprint,
  args: { value: "sha256:2069e386bdca701da7782dd0b2b29bd5c79151d90bcfc52f30a9678f97ac2aac" },
} satisfies Meta<typeof Fingerprint>

export default meta

export const PlanFingerprint: StoryObj<typeof meta> = {}
export const ArtifactDigest: StoryObj<typeof meta> = { args: { label: "artifact" } }
