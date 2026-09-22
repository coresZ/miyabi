import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  isHLSProvider,
  MediaPlayer,
  MediaProvider,
  useMediaPlayer,
  useMediaState,
  type MediaPlayerInstance
} from '@vidstack/react'
import { DefaultVideoLayout } from '@vidstack/react/player/layouts/default'

import type { WatchHistoryScope, WatchResume } from '@/api/watch-history'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { PlayerControlsVisibility } from './controls-visibility'
import { playerIcons } from './icons'
import {
  PlayerCloseButton,
  PlayerError,
  PlayerLoading,
  PlayerLoadingIndicator,
  PlayerTitle
} from './player-status'
import { playerTranslations } from './translations'
import { PlayerTimeSlider, PlayerVolumeSlider } from './sliders'
import { useHoldSpeed } from './use-hold-speed'
import { usePlaybackSource } from './use-playback-source'
import { useWatchProgress } from './use-watch-progress'

export function PlaybackPlayer({
  title,
  movieID,
  openingID,
  source: watchSource,
  fileID,
  history
}: {
  title: string
  movieID: number
  openingID: string
  source: WatchHistoryScope
  fileID: string
  history?: WatchResume
}) {
  const [player, setPlayer] = useState<MediaPlayerInstance | null>(null)
  const holdSpeed = useHoldSpeed(player)
  const progress = useWatchProgress(movieID, watchSource, fileID, history)

  const {
    playback,
    sources,
    source,
    loading,
    failed,
    autoPlay,
    retry,
    changeQuality,
    handleCanPlay,
    handleTimeUpdate,
    handlePlaying,
    handleSeeked,
    handlePause,
    handleEnded,
    handleError
  } = usePlaybackSource({ fileID, openingID, history, progress })

  return (
    <MediaPlayer
      ref={setPlayer}
      className="miyabi-player dark"
      title={title}
      src={loading || playback.isError || failed ? undefined : source}
      viewType="video"
      streamType="on-demand"
      autoPlay={autoPlay}
      playsInline
      onCanPlay={() => handleCanPlay(player)}
      onTimeUpdate={detail => handleTimeUpdate(detail.currentTime, player)}
      onPlaying={handlePlaying}
      onSeeked={currentTime => handleSeeked(currentTime, player)}
      onPause={() => handlePause(player)}
      onEnded={() => handleEnded(player)}
      onError={handleError}
      onProviderChange={provider => {
        if (isHLSProvider(provider)) provider.library = () => import('hls.js')
      }}
    >
      <PlayerControlsVisibility />
      <MediaProvider />
      {holdSpeed ? <div className="miyabi-player-feedback">倍速播放中</div> : null}
      {loading || playback.isError || failed ? (
        <div className="absolute inset-0 z-20 cursor-auto">
          {loading ? (
            <PlayerLoading title={title} />
          ) : (
            <PlayerError
              title={title}
              error={playback.error ?? undefined}
              message="播放中断，请重新加载播放地址。"
              onRetry={retry}
            />
          )}
        </div>
      ) : (
        <PlayerReady title={title}>
          <DefaultVideoLayout
            icons={playerIcons}
            translations={playerTranslations}
            colorScheme="dark"
            seekStep={5}
            noModal
            slots={{
              bufferingIndicator: null,
              googleCastButton: null,
              timeSlider: <PlayerTimeSlider />,
              volumeSlider: <PlayerVolumeSlider />,
              topControlsGroupStart: <PlayerTitle title={title} />,
              topControlsGroupEnd: <PlayerCloseButton />,
              chapterTitle: <div className="vds-controls-spacer" />,
              beforeSettingsMenu:
                sources.length > 1 && source ? (
                  <PlaybackQualitySelect
                    player={player}
                    value={source.src}
                    onValueChange={value => changeQuality(value, player)}
                  >
                    {sources.map(item => (
                      <SelectItem key={item.src} value={item.src}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </PlaybackQualitySelect>
                ) : null
            }}
          >
            <PlayerBufferingIndicator />
          </DefaultVideoLayout>
        </PlayerReady>
      )}
    </MediaPlayer>
  )
}

function PlayerReady({ title, children }: { title: string; children: ReactNode }) {
  const canPlay = useMediaState('canPlay')
  const player = useMediaPlayer()
  const focused = useRef(false)

  useEffect(() => {
    if (!canPlay || !player?.el || focused.current) return
    focused.current = true
    player.el.focus({ preventScroll: true })
  }, [canPlay, player])

  if (!canPlay) {
    return (
      <div className="absolute inset-0 z-20 cursor-auto">
        <PlayerLoading title={title} />
      </div>
    )
  }

  return children
}

function PlayerBufferingIndicator() {
  const waiting = useMediaState('waiting')

  if (!waiting) return null

  return (
    <div className="pointer-events-none absolute inset-0 z-20 grid place-items-center">
      <div className="rounded-full bg-background/75 px-4 py-2.5 backdrop-blur-xl">
        <PlayerLoadingIndicator message="正在缓冲…" />
      </div>
    </div>
  )
}

function PlaybackQualitySelect({
  player,
  value,
  onValueChange,
  children
}: {
  player: MediaPlayerInstance | null
  value: string
  onValueChange: (value: string) => void
  children: ReactNode
}) {
  return (
    <Select
      value={value}
      onValueChange={onValueChange}
      onOpenChange={open => {
        if (open) player?.controls.pause()
        else player?.controls.resume()
      }}
    >
      <SelectTrigger size="sm" className="max-w-full min-w-0 shrink-0 px-2 text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent
        container={player?.el}
        position="popper"
        align="end"
        side="top"
        collisionBoundary={player?.el}
        collisionPadding={12}
        className="max-w-[min(32rem,var(--radix-select-content-available-width))] bg-popover/90 backdrop-blur-xl"
      >
        {children}
      </SelectContent>
    </Select>
  )
}
