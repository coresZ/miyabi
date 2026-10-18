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

export async function setPlayerWindowMode(mode: PlayerWindowMode): Promise<void> {
  const call = invoke()
  if (!call) return
  await call('set_player_window_mode', { mode })
}
