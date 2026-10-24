import { Tabs as BaseTabs } from "@base-ui/react/tabs"
import type { ReactNode } from "react"

export interface TabItem<T extends string> {
  value: T
  label: string
  /** A count beside the label, such as how many Events there are. */
  count?: number
}

export interface TabsProps<T extends string> {
  /** What the tabs choose between, for assistive technology. */
  label: string
  tabs: TabItem<T>[]
  value: T
  onChange: (value: T) => void
  /** The panel of the tab that is open; only that one is rendered. */
  children: ReactNode
}

/**
 * Panels of one page, one open at a time. The arrow keys move between tabs
 * and Enter or Space opens one, as the ARIA tabs pattern asks; the page keeps
 * which one is open, so it can live in the address.
 */
export function Tabs<T extends string>({ label, tabs, value, onChange, children }: TabsProps<T>) {
  return (
    <BaseTabs.Root
      value={value}
      onValueChange={(next) => onChange(next as T)}
      className="flex flex-col gap-4"
    >
      <BaseTabs.List
        aria-label={label}
        className="relative flex gap-1 overflow-x-auto border-b border-line"
      >
        {tabs.map((tab) => (
          <BaseTabs.Tab
            key={tab.value}
            value={tab.value}
            className="inline-flex min-h-10 cursor-pointer items-center gap-1.5 rounded-t-lg border-0 bg-transparent px-3 text-[13.5px] font-medium whitespace-nowrap text-fg2 focus-visible:outline-2 focus-visible:outline-pri data-[active]:text-fg"
          >
            {tab.label}
            {/* A space keeps the count a word of its own in the tab's name. */}
            {tab.count === undefined ? null : " "}
            {tab.count === undefined ? null : (
              <span className="font-mono text-[11px] text-mute">{tab.count}</span>
            )}
          </BaseTabs.Tab>
        ))}
        <BaseTabs.Indicator className="absolute bottom-0 left-[var(--active-tab-left)] h-0.5 w-[var(--active-tab-width)] rounded-full bg-pri transition-[left,width] duration-200 motion-reduce:transition-none" />
      </BaseTabs.List>
      {tabs.map((tab) => (
        <BaseTabs.Panel
          key={tab.value}
          value={tab.value}
          className="flex flex-col gap-4 rounded-md focus-visible:outline-2 focus-visible:outline-pri"
        >
          {tab.value === value ? children : null}
        </BaseTabs.Panel>
      ))}
    </BaseTabs.Root>
  )
}
