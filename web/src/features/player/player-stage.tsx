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
  seekBy: (delta: number) => void
  setVolume: (value: number) => void
  stop: () => void
  setControlsHidden: (hidden: boolean) => void
  isControlsHidden: () => boolean
  toggleFullscreen: () => void
  setFullscreenState: (active: boolean) => void
  destroy: () => void
}

declare global {
  interface Window {
    MiyabiPlayer?: {
      create: (element: HTMLElement, options: Record<string, unknown>) => PlayerInstance
    }
  }
}

export type PlayerPrefs = {
  volume: number
  muted: boolean
  rate: number
}

type PlayerStageProps = {
  className?: string
  poster?: string
  prefs?: PlayerPrefs
  onReady?: (player: PlayerInstance) => void
  onChange?: (index: number, source: PlayerTrack) => void
  onEnded?: (index: number, hasNext: boolean) => void
  onProgress?: (currentTime: number, duration: number) => void
  onMetadata?: (width: number, height: number) => void
  onPrefsChange?: (prefs: PlayerPrefs) => void
  onFullscreenChange?: (active: boolean) => void
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
          volume: current.prefs?.volume,
          muted: current.prefs?.muted,
          rate: current.prefs?.rate,
          // Read through the ref: these callbacks close over page state that
          // changes after the player is created.
          onChange: (index: number, source: PlayerTrack) =>
            handlers.current.onChange?.(index, source),
          onEnded: (index: number, _source: unknown, hasNext: boolean) =>
            handlers.current.onEnded?.(index, hasNext),
          onProgress: (currentTime: number, duration: number) =>
            handlers.current.onProgress?.(currentTime, duration),
          onMetadata: (width: number, height: number) =>
            handlers.current.onMetadata?.(width, height),
          onPrefsChange: (prefs: PlayerPrefs) => handlers.current.onPrefsChange?.(prefs),
          onFullscreenChange: (active: boolean) => handlers.current.onFullscreenChange?.(active)
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
      className={cn(
        'aspect-video w-full overflow-hidden rounded-2xl bg-black lg:h-full',
        props.className
      )}
    >
      {/* The player kernel owns this element's class (`.miyabi-player`). Keeping
          it separate from the sized wrapper stops React from clobbering it on
          mode changes, which used to collapse the player height. */}
      <div ref={hostRef} className="size-full" />
    </div>
  )
}
