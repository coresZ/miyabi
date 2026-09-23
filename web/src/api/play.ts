import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'

import { apiDelete, apiGet, apiPatch, apiPost, apiPut } from '@/api/client'
import type { LibraryFile } from '@/api/library'
import type { WatchHistoryScope, WatchResume } from '@/api/watch-history'

export type SubtitleTrack = {
  id: number
  movie_id: number
  file_id?: string
  name: string
  display_name: string
  language: string
  format: string
  version_tag: string
  source: string
  offset_ms: number
  is_default: boolean
  src: string
}

export type SubtitleCandidate = {
  source: string
  name: string
  display_name: string
  language: string
  version: string
  url: string
  ext: string
  score: number
}

export type PlayFiles = {
  code: string
  title: string
  files: LibraryFile[]
  source: WatchHistoryScope
  resume?: WatchResume
  subtitles?: SubtitleTrack[]
}

export type PlaySource = {
  src: string
  type: 'application/x-mpegurl'
  label: string
}

type Playback = {
  id: string
  sources: PlaySource[]
}

const playQueryOptions = {
  gcTime: 0,
  staleTime: Infinity,
  refetchOnReconnect: false
} as const

export function usePlayFiles(movieID: number, openingID: string) {
  return useQuery({
    ...playQueryOptions,
    queryKey: ['play', 'files', movieID, openingID],
    queryFn: ({ signal }) => apiGet<PlayFiles>('/api/play/files', { movie_id: movieID }, signal)
  })
}

export function usePlayback(fileID: string, openingID: string) {
  const query = useQuery({
    ...playQueryOptions,
    queryKey: ['play', 'source', fileID, openingID],
    queryFn: ({ signal }) =>
      apiGet<Playback>(`/api/play/${encodeURIComponent(fileID)}`, undefined, signal)
  })
  const id = query.data?.id

  useEffect(() => {
    if (!id) return
    return () => {
      // The player aborts media requests on unmount; also release the server's URL registry.
      // Abandoned tabs and failed cleanup requests expire on the server.
      void apiDelete(`/api/play/${id}`).catch(() => {})
    }
  }, [id])

  return query
}

export function searchSubtitles(code: string, uncensored?: boolean, signal?: AbortSignal) {
  return apiGet<SubtitleCandidate[]>(
    '/api/subtitles/search',
    { code, uncensored: uncensored ? 'true' : undefined },
    signal
  )
}

export function applySubtitle(movieID: number, candidate: SubtitleCandidate) {
  return apiPost<SubtitleTrack>('/api/subtitles/apply', { movie_id: movieID, candidate })
}

export function updateSubtitleOffset(id: number, offsetMs: number) {
  return apiPatch<{ updated: boolean; offset_ms: number }>(`/api/subtitles/${id}/offset`, {
    offset_ms: offsetMs
  })
}

export function setDefaultSubtitle(id: number, movieID: number) {
  return apiPut<{ updated: boolean }>(`/api/subtitles/${id}/default`, { movie_id: movieID })
}

export function deleteSubtitle(id: number) {
  return apiDelete<{ deleted: boolean }>(`/api/subtitles/${id}`)
}
