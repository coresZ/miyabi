type TauriInvoke = (cmd: string, args?: Record<string, unknown>) => Promise<unknown>

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

/** Desktop: frameless immersive window, fitted to the source aspect by the shell. */
export async function enterPlayerImmersive(aspect?: number | null): Promise<void> {
  const call = invoke()
  if (!call) return
  await call('enter_player_immersive', { aspect: aspect ?? null })
}

/** Restore the exact window state captured before entering immersive mode. */
export async function restoreNormalWindow(): Promise<void> {
  const call = invoke()
  if (!call) return
  await call('restore_normal_window')
}

export type PlayerResizeDirection = 'NorthWest' | 'SouthEast'

/** Resize the frameless immersive window from one of its two corner handles. */
export async function startWindowResize(direction: PlayerResizeDirection): Promise<void> {
  const w = window as Window & {
    __TAURI__?: {
      window?: {
        getCurrentWindow?: () => {
          startResizeDragging?: (direction: PlayerResizeDirection) => Promise<void>
        }
      }
    }
  }
  await w.__TAURI__?.window?.getCurrentWindow?.().startResizeDragging?.(direction)
}
