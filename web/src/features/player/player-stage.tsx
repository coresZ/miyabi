import { cn } from 'cn'
import { useCallback, useEffect, useRef, useState } from 'react'

import { loadPlayerAssets } from './player-assets'

export type PlayerTrack = {
  url: string
  name: string
  code?: string
}

export type PlayerInstance = {
  setSources: (sources: { url: string; name: string }[], startIndex?: number) => void
  setQueue: (sources: { url: string; name: string }[]) => void
  next: () => void
  prev: () => void
  seek: (seconds: number) => void
  stop: () => void
  setControlsHidden: (hidden: boolean) => void
  isControlsHidden: () => boolean
  destroy: () => void
}

declare global {
  interface Window {
    MiyabiPlayer?: {
      create: (element: HTMLElement, options: Record<string, unknown>) => PlayerInstance
    }
  }
}

type PlayerStageProps = {
  className?: string
  poster?: string
  onReady?: (player: PlayerInstance) => void
  onChange?: (index: number, source: PlayerTrack) => void
  onEnded?: (index: number, hasNext: boolean) => void
  onProgress?: (currentTime: number, duration: number) => void
}

export function PlayerStage(props: PlayerStageProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string>()
  const handlers = useRef(props)
  useEffect(() => {
    handlers.current = props
  }, [props])

  const reportError = useCallback((reason: unknown) => {
    setError(reason instanceof Error ? reason.message : String(reason))
  }, [])

  useEffect(() => {
    let cancelled = false
    let instance: PlayerInstance | null = null
    loadPlayerAssets()
      .then(() => {
        if (cancelled || !hostRef.current || !window.MiyabiPlayer) return
        const current = handlers.current
        instance = window.MiyabiPlayer.create(hostRef.current, {
          sources: [],
          poster: current.poster,
          onChange: (index: number, source: PlayerTrack) => current.onChange?.(index, source),
          onEnded: (index: number, _source: unknown, hasNext: boolean) =>
            current.onEnded?.(index, hasNext),
          onProgress: (currentTime: number, duration: number) =>
            current.onProgress?.(currentTime, duration)
        })
        current.onReady?.(instance)
      })
      .catch(reportError)
    return () => {
      cancelled = true
      instance?.destroy()
    }
  }, [reportError])

  if (error) {
    return (
      <div
        className={cn(
          'grid aspect-video w-full place-items-center rounded-2xl bg-black text-sm text-muted-foreground',
          props.className
        )}
      >
        {error}
      </div>
    )
  }
  return (
    <div
      ref={hostRef}
      className={cn('aspect-video w-full overflow-hidden rounded-2xl bg-black', props.className)}
    />
  )
}
