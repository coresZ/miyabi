import { MonitorPlayIcon } from 'lucide-react'

import { Separator } from '@/components/ui/separator'
import { updateUiState, useUiState, type ImmersiveSize } from '@/stores/ui-state'
import { cn } from 'cn'

import { isDesktopShell } from './desktop-bridge'
import { SettingsSection } from './shared'

type ImmersiveSizeOption = {
  value: ImmersiveSize
  label: string
  size: string
  detail: string
  recommended?: boolean
}

const IMMERSIVE_SIZES: ImmersiveSizeOption[] = [
  {
    value: 'height',
    label: '高度优先',
    size: '1422 × 800',
    detail: '高度不变 · 宽度 +11% · 无黑边',
    recommended: true
  },
  {
    value: 'width',
    label: '宽度优先',
    size: '1280 × 720',
    detail: '宽度不变 · 高度 -80 · 无黑边'
  },
  {
    value: 'near',
    label: '接近原尺寸',
    size: '1408 × 792',
    detail: '宽度 +10% · 高度 -8 · 无黑边'
  }
]

export function PlayerSection() {
  if (!isDesktopShell()) return null
  return (
    <>
      <Separator />
      <PlayerSettings />
    </>
  )
}

function PlayerSettings() {
  const immersiveSize = useUiState(state => state.player.immersiveSize)

  return (
    <SettingsSection icon={<MonitorPlayIcon className="size-4" />} title="播放器">
      <div className="space-y-3">
        <div>
          <div className="text-sm font-medium">沉浸窗口尺寸</div>
          <p className="text-xs leading-5 text-muted-foreground">
            只影响进入沉浸时的初始窗口；正常播放始终 1280×800 / 100%，不会缩放。三种尺寸都保持
            16:9、无黑边。
          </p>
        </div>
        <div className="grid gap-2 sm:grid-cols-3" role="radiogroup" aria-label="沉浸窗口尺寸">
          {IMMERSIVE_SIZES.map(option => {
            const active = option.value === immersiveSize
            return (
              <button
                key={option.value}
                type="button"
                role="radio"
                aria-checked={active}
                onClick={() =>
                  updateUiState(current => ({
                    player: { ...current.player, immersiveSize: option.value }
                  }))
                }
                className={cn(
                  'rounded-lg border p-3 text-left transition-colors',
                  active
                    ? 'border-primary bg-primary/5 ring-1 ring-primary/30'
                    : 'border-border hover:bg-muted/50'
                )}
              >
                <span className="flex items-center gap-2">
                  <span
                    className={cn(
                      'size-2.5 flex-none rounded-full border',
                      active ? 'border-primary bg-primary' : 'border-muted-foreground/40'
                    )}
                  />
                  <span className="text-xs font-medium">{option.label}</span>
                  {option.recommended ? (
                    <span className="ml-auto rounded border border-border px-1.5 py-0.5 text-[10px] text-muted-foreground">
                      推荐
                    </span>
                  ) : null}
                </span>
                <span className="mt-2 block font-mono text-sm">{option.size}</span>
                <span className="mt-1 block text-[11px] leading-4 text-muted-foreground">
                  {option.detail}
                </span>
              </button>
            )
          })}
        </div>
        <p className="text-[11px] leading-4 text-muted-foreground">
          选择保存到设备偏好（ui-state.json），下次进入沉浸生效；四角比例缩放保留。
        </p>
      </div>
    </SettingsSection>
  )
}
