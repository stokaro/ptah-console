// Modeled on denisvmedia/inventario's frontend configuration, plus the rules
// that keep cluster data inert: everything the console shows came from a
// cluster or a person, and it renders as text.
import js from "@eslint/js"
import prettier from "eslint-config-prettier"
import jsxA11y from "eslint-plugin-jsx-a11y"
import reactPlugin from "eslint-plugin-react"
import reactHooks from "eslint-plugin-react-hooks"
import globals from "globals"
import tseslint from "typescript-eslint"

// Markup and code from strings. A plan's SQL, an Event's message or a log line
// passed through any of these would run in the person's session.
const inertData = [
  {
    selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
    message: "Cluster data renders as text. dangerouslySetInnerHTML is not used in the console.",
  },
  {
    selector: "AssignmentExpression[left.property.name=/^(innerHTML|outerHTML)$/]",
    message: "Cluster data renders as text. Do not assign innerHTML or outerHTML.",
  },
  {
    selector: "CallExpression[callee.property.name='insertAdjacentHTML']",
    message: "Cluster data renders as text. Do not use insertAdjacentHTML.",
  },
  {
    selector: "CallExpression[callee.name='eval']",
    message: "No eval: the Content Security Policy forbids it and nothing needs it.",
  },
  {
    selector: "NewExpression[callee.name='Function']",
    message: "No new Function: the Content Security Policy forbids it and nothing needs it.",
  },
]

// The shared components own the dialog and select primitives, so focus,
// labelling and escape handling are written once.
const primitivesInUi = [
  {
    selector: "JSXOpeningElement[name.name=/^(dialog|select)$/]",
    message:
      "Use the dialog and select components from @ptah/console-ui; raw <dialog> and <select> live only there.",
  },
]

export default [
  {
    ignores: [
      "**/node_modules/**",
      "**/dist/**",
      "**/coverage/**",
      "app/src/gen/**",
      "stub/**",
      "**/storybook-static/**",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: { ...globals.browser, ...globals.es2022 },
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    plugins: {
      react: reactPlugin,
      "react-hooks": reactHooks,
      "jsx-a11y": jsxA11y,
    },
    settings: {
      // eslint-plugin-react's "detect" calls an API ESLint 10 removed; pin the
      // React version instead and move it with React.
      react: { version: "19.3" },
    },
    rules: {
      ...reactPlugin.configs.recommended.rules,
      ...reactPlugin.configs["jsx-runtime"].rules,
      ...reactHooks.configs.recommended.rules,
      ...jsxA11y.configs.recommended.rules,
      "react/prop-types": "off",
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
      "@typescript-eslint/no-explicit-any": "error",
      "no-restricted-syntax": ["error", ...inertData, ...primitivesInUi],
    },
  },
  {
    files: ["packages/ui/src/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-syntax": ["error", ...inertData],
    },
  },
  {
    files: ["**/*.test.{ts,tsx}", "test/**/*.ts"],
    languageOptions: {
      globals: { ...globals.browser, ...globals.node, ...globals.es2022 },
    },
  },
  {
    files: ["*.config.{js,ts}", "app/*.config.{js,ts,cjs}"],
    languageOptions: {
      globals: { ...globals.node, ...globals.es2022 },
    },
  },
  {
    files: ["**/*.cjs"],
    languageOptions: {
      sourceType: "commonjs",
      globals: { ...globals.node, ...globals.es2022 },
    },
  },
  prettier,
]
