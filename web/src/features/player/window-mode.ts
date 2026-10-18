type TauriInvoke = (cmd: string, args?: Record<string, unknown>) => Promise<unknown>

export type PlayerWindowMode = 'windowed' | 'borderless' | 'fullscreen'

function invoke(): TauriInvoke | null {
  const w = window as Window & {
    __TAURI__?: { core?: { invoke?: TauriInvoke } }
    __TAURI_INTERNALS__?: { invoke?: TauriInvoke }
  }
  return w.__TAURI__?.core?.invoke ?? w.__TAURI_INTERNALS__?.invoke ?? null
}

/** True only inside the desktop shell; the browser falls back to the plain page. */
export function isDesktopWindow(): boolean {
  return invoke() != null
}

/**
 * `aspect` (width / height) lets the shell size the borderless window to the
 * source, so the first frame fills the window instead of being letterboxed.
 */
export async function setPlayerWindowMode(
  mode: PlayerWindowMode,
  aspect?: number | null
): Promise<void> {
  const call = invoke()
  if (!call) return
  await call('set_player_window_mode', { mode, aspect: aspect ?? null })
}
