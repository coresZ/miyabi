import { useRef, useState } from 'react'
import type { MediaPlayerInstance } from '@vidstack/react'

import { usePlayback } from '@/api/play'
import type { WatchResume } from '@/api/watch-history'
import { watchResumePosition } from '@/lib/watch-progress'
import type { useWatchProgress } from './use-watch-progress'

export function usePlaybackSource({
  fileID,
  openingID,
  history,
  progress
}: {
  fileID: string
  openingID: string
  history?: WatchResume
  progress: ReturnType<typeof useWatchProgress>
}) {
  const playback = usePlayback(fileID, openingID)
  const [selectedSrc, setSelectedSrc] = useState<string>()
  const [failed, setFailed] = useState(false)
  const [autoPlay, setAutoPlay] = useState(true)

  const initialPosition = watchResumePosition(history, fileID)
  const position = useRef(initialPosition)
  const resumeTime = useRef<number | null>(initialPosition)
  const playing = useRef(false)

  const sources = playback.data?.sources ?? []
  const source = sources.find(item => item.src === selectedSrc) ?? sources[0]
  const loading = playback.isPending || playback.isFetching

  const retry = () => {
    resumeTime.current = position.current
    setFailed(false)
    setAutoPlay(true)
    void playback.refetch()
  }

  const changeQuality = (value: string, player: MediaPlayerInstance | null) => {
    resumeTime.current = position.current
    setAutoPlay(!player?.paused)
    setSelectedSrc(value)
  }

  const handleCanPlay = (player: MediaPlayerInstance | null) => {
    if (player && resumeTime.current !== null) {
      const target = Math.min(resumeTime.current, Math.max(0, player.duration - 1))
      if (target > 0) player.remoteControl.seek(target)
      else resumeTime.current = null
    }
  }

  const handleTimeUpdate = (currentTime: number, player: MediaPlayerInstance | null) => {
    if (resumeTime.current !== null || !player || !playing.current) return
    position.current = currentTime
    progress?.update(currentTime, player.duration)
  }

  const handlePlaying = () => {
    playing.current = true
  }

  const handleSeeked = (currentTime: number, player: MediaPlayerInstance | null) => {
    resumeTime.current = null
    position.current = currentTime
    if (player) progress?.update(currentTime, player.duration)
    void progress?.flush(true)
  }

  const handlePause = (player: MediaPlayerInstance | null) => {
    playing.current = false
    if (player && resumeTime.current === null) {
      progress?.update(position.current, player.duration)
    }
    void progress?.flush(true)
  }

  const handleEnded = (player: MediaPlayerInstance | null) => {
    playing.current = false
    if (player) progress?.update(player.duration, player.duration)
    void progress?.flush(true)
  }

  const handleError = () => {
    resumeTime.current = position.current
    setFailed(true)
  }

  return {
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
  }
}
