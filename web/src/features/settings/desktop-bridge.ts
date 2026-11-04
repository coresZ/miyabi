type TauriInvoke = (cmd: string, args?: Record<string, unknown>) => Promise<unknown>

export type DesktopConfig = {
  portable: boolean
  dataDir: string
  lan: boolean
  port: number
  hasPassword: boolean
  addresses: string[]
  localUrl: string
}

export function tauriInvoke(): TauriInvoke | null {
  const w = window as Window & {
    __TAURI__?: { core?: { invoke?: TauriInvoke } }
    __TAURI_INTERNALS__?: { invoke?: TauriInvoke }
  }
  return w.__TAURI__?.core?.invoke ?? w.__TAURI_INTERNALS__?.invoke ?? null
}

export function isDesktopShell(): boolean {
  return tauriInvoke() != null
}

export async function desktopConfig(): Promise<DesktopConfig> {
  const invoke = tauriInvoke()
  if (!invoke) throw new Error('请在 Miyabi 桌面窗口中打开')
  return (await invoke('desktop_config')) as DesktopConfig
}

export async function applyDesktop(input: {
  lan: boolean
  port: number
  password: string | null
}): Promise<void> {
  const invoke = tauriInvoke()
  if (!invoke) throw new Error('请在 Miyabi 桌面窗口中打开')
  await invoke('apply_desktop', input)
}
