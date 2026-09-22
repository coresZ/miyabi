import { useEffect, useMemo } from 'react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import { useMarkMovieWatched } from '@/api/library'
import {
  saveWatchProgress,
  type WatchHistoryScope,
  type WatchResume,
  type WatchSession
} from '@/api/watch-history'
import { watchSessions } from './watch-progress-writer'

export function useWatchProgress(
  movieID: number,
  source: WatchHistoryScope,
  fileID: string,
  resume: WatchResume | undefined
) {
  const { mutateAsync: markWatched } = useMarkMovieWatched()
  const writer = useMemo(
    () =>
      createPlayerProgressWriter(movieID, source, fileID, resume, async () => {
        try {
          return (await markWatched({ movieID, source })).history
        } catch (error) {
          toast.error('观看记录保存失败', {
            id: 'library:watched-error',
            description: '播放仍可继续。请检查后端连接，稍后重新打开影片重试。'
          })
          throw error
        }
      }),
    [movieID, source, fileID, resume, markWatched]
  )

  useEffect(() => {
    void writer.start().catch(() => {})
    const flush = () => {
      void writer.flush(true)
    }
    const visibilityChanged = () => {
      if (document.visibilityState === 'hidden') flush()
    }
    window.addEventListener('pagehide', flush)
    window.addEventListener('online', flush)
    document.addEventListener('visibilitychange', visibilityChanged)
    return () => {
      window.removeEventListener('pagehide', flush)
      window.removeEventListener('online', flush)
      document.removeEventListener('visibilitychange', visibilityChanged)
      flush()
    }
  }, [writer])

  return writer
}

function createPlayerProgressWriter(
  movieID: number,
  source: WatchHistoryScope,
  fileID: string,
  resume: WatchResume | undefined,
  start: () => Promise<WatchSession>
) {
  let active = true
  let registered = false
  let errorShown = false
  const toastID = `history:progress:${movieID}`
  return watchSessions.create({
    movieID,
    source,
    resume,
    fileID,
    start: async () => {
      const session = await start()
      registered = true
      return session
    },
    write: async (historyID, progress, keepalive) => {
      if (!active) return
      try {
        await saveWatchProgress(historyID, progress, keepalive)
      } catch (error) {
        // Cleared history and switched sources invalidate this playback session.
        if (error instanceof ApiError && error.status < 500) {
          active = false
          return
        }
        throw error
      }
    },
    onError: () => {
      if (!registered || errorShown) return
      errorShown = true
      toast.error('播放进度暂未同步', { id: toastID, description: '恢复连接后会重试保存。' })
    },
    onSaved: () => {
      if (!errorShown) return
      errorShown = false
      toast.dismiss(toastID)
    }
  })
}
