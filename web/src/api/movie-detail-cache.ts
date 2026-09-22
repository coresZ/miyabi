import { queryOptions, type QueryClient } from '@tanstack/react-query'

import type { DiscoverMovie, DiscoverMovieDetail } from './discover'

export const discoverKeys = {
  all: ['discover'] as const,
  movies: (params?: unknown) => ['discover', 'movies', params] as const,
  movie: (id: string) => ['discover', 'movie', id] as const,
  magnets: (id: string) => ['discover', 'movie', id, 'magnets'] as const,
  search: (params?: unknown) => ['discover', 'search', params] as const,
  tags: (zone: string) => ['discover', 'tags', zone] as const,
  route: ['javdb', 'route'] as const
}

const detailStaleTime = 5 * 60_000

type CardEntry = {
  card: DiscoverMovie
  updatedAt: number
  isInvalidated: boolean
}

type ClientIndex = {
  cards: Map<string, CardEntry>
}

const clientIndexes = new WeakMap<QueryClient, ClientIndex>()

function getClientIndex(client: QueryClient): ClientIndex {
  let index = clientIndexes.get(client)
  if (!index) {
    const activeIndex: ClientIndex = { cards: new Map() }
    index = activeIndex
    clientIndexes.set(client, index)

    const cache = client.getQueryCache()
    const updateFromQuery = (query: ReturnType<typeof cache.findAll>[number]) => {
      const key = query.queryKey
      if (!Array.isArray(key) || key[0] !== 'discover') return
      const kind = key[1]
      const { data, dataUpdatedAt, isInvalidated } = query.state
      if (!data) return

      if (kind === 'movie' && key.length === 3 && typeof key[2] === 'string') {
        const id = key[2]
        const card = data as DiscoverMovieDetail
        const existing = activeIndex.cards.get(id)
        if (!existing || dataUpdatedAt >= existing.updatedAt) {
          activeIndex.cards.set(id, { card, updatedAt: dataUpdatedAt, isInvalidated })
        }
      } else if (kind === 'movies' || kind === 'search') {
        if (Array.isArray(data)) {
          for (const item of data as DiscoverMovie[]) {
            if (item && item.id) {
              const existing = activeIndex.cards.get(item.id)
              if (!existing || dataUpdatedAt >= existing.updatedAt) {
                activeIndex.cards.set(item.id, {
                  card: item,
                  updatedAt: dataUpdatedAt,
                  isInvalidated
                })
              }
            }
          }
        }
      }
    }

    const rebuild = () => {
      activeIndex.cards.clear()
      for (const query of cache.findAll({ queryKey: discoverKeys.all })) {
        updateFromQuery(query)
      }
    }

    rebuild()

    cache.subscribe(event => {
      if (event.type === 'updated' || event.type === 'added') {
        updateFromQuery(event.query)
      } else if (event.type === 'removed') {
        rebuild()
      }
    })
  }
  return index
}

// List results already contain everything a card needs. Read them in place;
// never seed an incomplete list item into the full-detail query.
export function findCachedMovieCard(client: QueryClient, id: string, freshOnly = false) {
  const index = getClientIndex(client)
  const entry = index.cards.get(id)
  if (!entry) return undefined

  if (freshOnly) {
    const oldest = Date.now() - detailStaleTime
    if (entry.updatedAt < oldest || entry.isInvalidated) {
      return undefined
    }
  }
  return entry.card
}

type DetailRequest = { id: string; consumers: number; started: boolean }
type DetailQueue = { requests: Map<string, DetailRequest>; running: number }

export function createMovieDetailLoader(
  fetchDetail: (id: string, signal?: AbortSignal) => Promise<DiscoverMovieDetail>
) {
  const queues = new WeakMap<QueryClient, DetailQueue>()

  function options(id: string) {
    return queryOptions({
      queryKey: discoverKeys.movie(id),
      queryFn: ({ signal }) => fetchDetail(id, signal),
      staleTime: detailStaleTime,
      retry: false,
      refetchOnWindowFocus: false
    })
  }

  function queueFor(client: QueryClient) {
    let queue = queues.get(client)
    if (!queue) {
      queue = { requests: new Map(), running: 0 }
      queues.set(client, queue)
    }
    return queue
  }

  function prefetchOptions(id: string) {
    // A started prefetch survives the brief observer gap during navigation.
    // Normal foreground queries still consume their signal and can be canceled.
    return { ...options(id), queryFn: () => fetchDetail(id) }
  }

  function start(
    client: QueryClient,
    queue: DetailQueue,
    request: DetailRequest,
    background: boolean
  ) {
    request.started = true
    if (background) queue.running++
    void client.prefetchQuery(prefetchOptions(request.id)).finally(() => {
      if (queue.requests.get(request.id) === request) queue.requests.delete(request.id)
      if (background) queue.running--
      pump(client, queue)
    })
  }

  function pump(client: QueryClient, queue: DetailQueue) {
    for (const request of queue.requests.values()) {
      if (request.started) continue
      if (findCachedMovieCard(client, request.id, true)) {
        queue.requests.delete(request.id)
        continue
      }
      if (queue.running >= 2) break
      start(client, queue, request, true)
    }
  }

  function request(client: QueryClient, id: string) {
    if (
      findCachedMovieCard(client, id, true) ||
      client.getQueryState(discoverKeys.movie(id))?.status === 'error'
    )
      return () => {}
    const queue = queueFor(client)
    let pending = queue.requests.get(id)
    if (!pending) {
      pending = { id, consumers: 0, started: false }
      queue.requests.set(id, pending)
    }
    const current = pending
    current.consumers++
    queueMicrotask(() => pump(client, queue))
    let released = false
    return () => {
      if (released) return
      released = true
      current.consumers--
      if (!current.started && current.consumers === 0 && queue.requests.get(id) === current) {
        queue.requests.delete(id)
      }
    }
  }

  function prioritize(client: QueryClient, id: string) {
    const queue = queues.get(client)
    const pending = queue?.requests.get(id)
    if (!queue || !pending) return false
    if (!pending.started) start(client, queue, pending, false)
    return true
  }

  function prefetch(client: QueryClient, id: string) {
    if (!prioritize(client, id)) void client.prefetchQuery(prefetchOptions(id))
  }

  return { options, request, prioritize, prefetch }
}
