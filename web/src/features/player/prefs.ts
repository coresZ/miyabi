export type PlayerPrefs = {
  volume: number
  muted: boolean
  rate: number
}

export const DEFAULT_PLAYER_PREFS: PlayerPrefs = { volume: 1, muted: false, rate: 1 }

const STORAGE_KEY = 'miyabi.player.prefs'

export function readPlayerPrefs(): PlayerPrefs {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULT_PLAYER_PREFS
    const parsed = JSON.parse(raw) as Partial<PlayerPrefs>
    return {
      volume: Number.isFinite(parsed.volume) ? Math.min(1, Math.max(0, Number(parsed.volume))) : 1,
      muted: Boolean(parsed.muted),
      rate: Number.isFinite(parsed.rate) ? Math.min(4, Math.max(0.25, Number(parsed.rate))) : 1
    }
  } catch {
    return DEFAULT_PLAYER_PREFS
  }
}

export function savePlayerPrefs(prefs: PlayerPrefs): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs))
  } catch {
    // Private mode or a full quota: preferences simply do not persist.
  }
}
