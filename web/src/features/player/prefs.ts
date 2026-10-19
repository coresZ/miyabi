import { getUiState, updateUiState } from '@/stores/ui-state'

export type PlayerPrefs = {
  volume: number
  muted: boolean
  rate: number
}

export const DEFAULT_PLAYER_PREFS: PlayerPrefs = { volume: 1, muted: false, rate: 1 }

/** Reads the player slice of the device UI state (loaded before first render). */
export function readPlayerPrefs(): PlayerPrefs {
  return getUiState().player
}

export function savePlayerPrefs(prefs: PlayerPrefs): void {
  updateUiState({ player: prefs })
}
