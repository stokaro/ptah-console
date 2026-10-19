import type { Decorator, Preview } from "@storybook/react-vite"

import "../src/index.css"

// Every story renders in the theme picked in the toolbar, on the page
// background, the way the console draws it.
const withTheme: Decorator = (Story, context) => {
  document.documentElement.setAttribute(
    "data-theme",
    context.globals.theme === "dark" ? "dark" : "light"
  )
  return (
    <div className="bg-bg p-6 text-fg">
      <Story />
    </div>
  )
}

const preview: Preview = {
  decorators: [withTheme],
  globalTypes: {
    theme: {
      description: "Color theme",
      toolbar: {
        title: "Theme",
        items: [
          { value: "light", title: "Light" },
          { value: "dark", title: "Dark" },
        ],
        dynamicTitle: true,
      },
    },
  },
  initialGlobals: { theme: "light" },
  parameters: {
    layout: "fullscreen",
    a11y: { test: "error" },
  },
}

export default preview
