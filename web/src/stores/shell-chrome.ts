import { create } from 'zustand'

/** Lets a full-screen page (the player) hide the floating navigation. */
type ShellChromeState = {
  navHidden: boolean
  setNavHidden: (hidden: boolean) => void
}

export const useShellChrome = create<ShellChromeState>(set => ({
  navHidden: false,
  setNavHidden: navHidden => set({ navHidden })
}))
