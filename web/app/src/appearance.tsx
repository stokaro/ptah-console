import { createContext, useContext, useEffect, useState, type ReactNode } from "react"

import { applyAccent, readAccent, storeAccent, type AccentChoice } from "./accent"
import { followTheme, readTheme, storeTheme, type ThemeChoice } from "./theme"

interface Appearance {
  theme: ThemeChoice
  setTheme: (choice: ThemeChoice) => void
  accent: AccentChoice
  setAccent: (choice: AccentChoice) => void
}

const AppearanceContext = createContext<Appearance | undefined>(undefined)

/**
 * The viewer's theme and accent, read from storage once, applied to the
 * document and stored on every change. Every switch reads the same state, so
 * a choice made in one place shows in all of them.
 */
export function AppearanceProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<ThemeChoice>(readTheme)
  const [accent, setAccentState] = useState<AccentChoice>(readAccent)
  useEffect(() => followTheme(theme), [theme])
  useEffect(() => applyAccent(accent), [accent])
  const value: Appearance = {
    theme,
    setTheme: (choice) => {
      storeTheme(choice)
      setThemeState(choice)
    },
    accent,
    setAccent: (choice) => {
      storeAccent(choice)
      setAccentState(choice)
    },
  }
  return <AppearanceContext.Provider value={value}>{children}</AppearanceContext.Provider>
}

function useAppearance() {
  const appearance = useContext(AppearanceContext)
  if (!appearance) {
    throw new Error("the theme and accent are read inside an AppearanceProvider")
  }
  return appearance
}

/** The theme choice, applied and stored. */
export function useTheme(): [ThemeChoice, (choice: ThemeChoice) => void] {
  const { theme, setTheme } = useAppearance()
  return [theme, setTheme]
}

/** The accent choice, applied and stored. */
export function useAccent(): [AccentChoice, (choice: AccentChoice) => void] {
  const { accent, setAccent } = useAppearance()
  return [accent, setAccent]
}
