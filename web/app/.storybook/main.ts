import type { StorybookConfig } from "@storybook/react-vite"

// The stories use the app's own Vite configuration, so they get the same
// stylesheet, tokens and fonts the console ships.
const config: StorybookConfig = {
  stories: ["../../packages/ui/src/**/*.stories.tsx", "../src/**/*.stories.tsx"],
  addons: ["@storybook/addon-a11y"],
  framework: { name: "@storybook/react-vite", options: {} },
  core: { disableTelemetry: true },
}

export default config
