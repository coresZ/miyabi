import { useSyncExternalStore } from 'react'

/**
 * Device UI preferences. The desktop shell keeps them in `ui-state.json` next
 * to `desktop.json`; a browser falls back to localStorage. Server settings
 * (Emby, network, sources, accounts) stay in the Go backend database.
 */
export type ImmersiveSize = 'height' | 'width' | 'near'

export type UiState = {
  nsfwMode: boolean
  theme: 'system' | 'light' | 'dark'
  player: { volume: number; muted: boolean; rate: number; immersiveSize: ImmersiveSize }
  gif: { fps: number; scale: number; loop: 'infinite' | 'once' }
}

export const DEFAULT_UI_STATE: UiState = {
  nsfwMode: true,
  theme: 'system',
  player: { volume: 1, muted: false, rate: 1, immersiveSize: 'height' },
  gif: { fps: 10, scale: 75, loop: 'infinite' }
}

const STORAGE_KEY = 'miyabi-ui-state'

type TauriInvoke = (cmd: string, args?: Record<string, unknown>) => Promise<unknown>

function invoke(): TauriInvoke | null {
  const w = window as Window & {
    __TAURI__?: { core?: { invoke?: TauriInvoke } }
    __TAURI_INTERNALS__?: { invoke?: TauriInvoke }
  }
  return w.__TAURI__?.core?.invoke ?? w.__TAURI_INTERNALS__?.invoke ?? null
}

let state: UiState = DEFAULT_UI_STATE
let loaded = false
let saveTimer: number | undefined
const listeners = new Set<() => void>()

function clamp(value: unknown, min: number, max: number, fallback: number): number {
  const number = Number(value)
  return Number.isFinite(number) ? Math.min(max, Math.max(min, number)) : fallback
}

function normalize(raw: unknown): UiState {
  const input = (raw ?? {}) as Partial<UiState>
  const player = input.player ?? DEFAULT_UI_STATE.player
  const gif = input.gif ?? DEFAULT_UI_STATE.gif
  return {
    nsfwMode: input.nsfwMode === undefined ? DEFAULT_UI_STATE.nsfwMode : Boolean(input.nsfwMode),
    theme: input.theme === 'light' || input.theme === 'dark' ? input.theme : 'system',
    player: {
      volume: clamp(player.volume, 0, 1, DEFAULT_UI_STATE.player.volume),
      muted: Boolean(player.muted),
      rate: clamp(player.rate, 0.25, 4, DEFAULT_UI_STATE.player.rate),
      immersiveSize:
        player.immersiveSize === 'width' || player.immersiveSize === 'near'
          ? player.immersiveSize
          : DEFAULT_UI_STATE.player.immersiveSize
    },
    gif: {
      fps: clamp(gif.fps, 1, 30, DEFAULT_UI_STATE.gif.fps),
      scale: clamp(gif.scale, 10, 100, DEFAULT_UI_STATE.gif.scale),
      loop: gif.loop === 'once' ? 'once' : 'infinite'
    }
  }
}

function readJson(key: string): unknown {
  try {
    const raw = window.localStorage.getItem(key)
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

function readRaw(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

function readBrowser(): unknown {
  const stored = readJson(STORAGE_KEY)
  if (stored) return stored

  // One-time bridge for browser users coming from the old per-key storage.
  const legacy: Partial<UiState> = {}
  const settings = readJson('miyabi-settings') as { state?: { nsfwMode?: boolean } } | null
  if (settings?.state?.nsfwMode !== undefined) legacy.nsfwMode = settings.state.nsfwMode
  const theme = readRaw('miyabi-theme')
  if (theme === 'light' || theme === 'dark' || theme === 'system') legacy.theme = theme
  const player = readJson('miyabi.player.prefs') as UiState['player'] | null
  if (player) legacy.player = player
  const gif = readJson('player.gifCapture.settings') as UiState['gif'] | null
  if (gif) legacy.gif = gif
  if (legacy.player && !legacy.player.rate) {
    const rate = Number(readRaw('playbackRate'))
    if (Number.isFinite(rate) && rate > 0) legacy.player.rate = rate
  }
  return legacy
}

function emit() {
  for (const listener of listeners) listener()
}

function flush() {
  const snapshot = state
  const call = invoke()
  if (call) {
    void call('save_ui_state', { value: snapshot }).catch(() => {})
    return
  }
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(snapshot))
  } catch {
    // Private mode or a full quota: preferences simply do not persist.
  }
}

function persist() {
  window.clearTimeout(saveTimer)
  saveTimer = window.setTimeout(flush, 250)
}

/** Loads the persisted state once, before React renders. */
export async function loadUiState(): Promise<void> {
  if (loaded) return
  loaded = true
  const call = invoke()
  if (!call) {
    try {
      state = normalize(readBrowser())
    } catch {
      state = normalize(null)
    }
    return
  }
  try {
    state = normalize(await call('load_ui_state'))
  } catch {
    try {
      state = normalize(readBrowser())
    } catch {
      state = normalize(null)
    }
  }
}

export function getUiState(): UiState {
  return state
}

export function updateUiState(patch: Partial<UiState> | ((current: UiState) => Partial<UiState>)) {
  const previous = state
  const next = typeof patch === 'function' ? patch(previous) : patch
  state = normalize({ ...previous, ...next })
  emit()
  // Discrete settings must survive an immediate close; slider-like values can
  // wait for the debounce.
  if (
    'nsfwMode' in next ||
    'theme' in next ||
    state.player.immersiveSize !== previous.player.immersiveSize
  ) {
    window.clearTimeout(saveTimer)
    flush()
  } else {
    persist()
  }
}

export function subscribeUiState(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useUiState<T>(selector: (state: UiState) => T): T {
  return useSyncExternalStore(
    subscribeUiState,
    () => selector(state),
    () => selector(DEFAULT_UI_STATE)
  )
}

declare global {
  interface Window {
    MiyabiUiState?: {
      getGif: () => UiState['gif']
      saveGif: (gif: UiState['gif']) => void
    }
  }
}

if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', flush)
  window.MiyabiUiState = {
    getGif: () => state.gif,
    saveGif: gif => updateUiState({ gif })
  }
}
