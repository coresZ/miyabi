import { useInfiniteQuery } from '@tanstack/react-query'

import { apiGet } from '@/api/client'
import type { LibrarySource } from '@/api/tasks'

export const PLAYABLE_PAGE_SIZE = 30

export type PlayableKind = 'local' | 'remote' | 'favorite'

export type PlayableVideo = {
  file_id: string
  name: string
  size: number
}

export type Playable = {
  movie_id: number
  code: string
  title: string
  poster?: string
  kind: PlayableKind
  videos: PlayableVideo[]
}

export type PlayablePage = {
  items: Playable[]
  source?: LibrarySource
  page: number
  has_more: boolean
}

export const playableKeys = {
  all: ['playables'] as const,
  page: (scope: string, directory: string, query: string) =>
    ['playables', scope, directory, query] as const
}

export function playableVideoUrl(kind: PlayableKind, fileID: string): string {
  const path = kind === 'remote' ? '/api/strm/play/' : '/api/local/play/'
  return path + encodeURIComponent(fileID)
}

export function usePlayables(scope: PlayableKind, directory: string, query: string) {
  return useInfiniteQuery({
    queryKey: playableKeys.page(scope, directory, query),
    initialPageParam: 1,
    queryFn: ({ pageParam, signal }) =>
      apiGet<PlayablePage>(
        '/api/playables',
        { scope, directory, q: query, page: pageParam, limit: PLAYABLE_PAGE_SIZE },
        signal
      ),
    getNextPageParam: last => (last.has_more ? last.page + 1 : undefined)
  })
}
