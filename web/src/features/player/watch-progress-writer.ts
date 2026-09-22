import type {
  WatchHistoryScope,
  WatchProgress,
  WatchResume,
  WatchSession
} from '@/api/watch-history'

type Position = Pick<WatchProgress, 'position' | 'duration'>
type ProgressSample = Omit<WatchProgress, 'session_id'>
type WriteProgress = (progress: ProgressSample, keepalive: boolean) => Promise<unknown>

const SAVE_INTERVAL = 10_000

export function createWatchProgressWriter({
  fileID,
  write,
  now = Date.now,
  onError,
  onSaved,
  onUpdate
}: {
  fileID: string
  write: WriteProgress
  now?: () => number
  onError?: (error: unknown) => void
  onSaved?: (position: Position) => void
  onUpdate?: (position: Position) => void
}) {
  let pending: Position | undefined
  let saved: Position | undefined
  let version = 0
  let savedVersion = 0
  let inFlight = 0
  let lastAttempt = now()
  let lastRequest:
    | { position: Position; keepalive: boolean; settled: boolean; promise: Promise<void> }
    | undefined

  function equal(left: Position | undefined, right: Position) {
    return left?.position === right.position && left.duration === right.duration
  }

  function flush(keepalive = false): Promise<void> {
    if (!pending || (inFlight === 0 && equal(saved, pending))) return Promise.resolve()
    if (!keepalive && inFlight > 0) return lastRequest?.promise ?? Promise.resolve()
    if (
      lastRequest &&
      !lastRequest.settled &&
      equal(lastRequest.position, pending) &&
      (!keepalive || lastRequest.keepalive)
    )
      return lastRequest.promise

    const position = pending
    const requestVersion = ++version
    lastAttempt = now()
    inFlight++
    const request = { position, keepalive, settled: false, promise: Promise.resolve() }
    let response: Promise<unknown>
    try {
      // Start keepalive requests inside pagehide, before the document is discarded.
      response = write({ ...position, file_id: fileID, version: requestVersion }, keepalive)
    } catch (error) {
      response = Promise.reject(error)
    }
    request.promise = response
      .then(() => {
        if (requestVersion >= savedVersion) {
          saved = position
          savedVersion = requestVersion
          onSaved?.(position)
        }
      })
      .catch(error => {
        if (requestVersion > savedVersion) onError?.(error)
      })
      .finally(() => {
        inFlight--
        request.settled = true
      })
    lastRequest = request
    return request.promise
  }

  function update(position: number, duration: number) {
    if (!Number.isFinite(position) || !Number.isFinite(duration) || duration <= 0) return
    pending = { position: Math.min(duration, Math.max(0, position)), duration }
    onUpdate?.(pending)
    if (now() - lastAttempt >= SAVE_INTERVAL) void flush()
  }

  return { update, flush }
}

// Session creation (including retries) waits for preceding progress writes.
// Progress within one session can overlap so pagehide sends keepalive immediately.
export function createWatchSessionQueue() {
  let tail: Promise<unknown> | undefined
  const pending = new Map<string, { owner: object; resume: WatchResume }>()

  function key(movieID: number, source: WatchHistoryScope) {
    return JSON.stringify([movieID, source.account_id, source.directory_id])
  }

  function track<T>(result: Promise<T>): Promise<T> {
    const barrier = Promise.allSettled(tail ? [tail, result] : [result]).then(() => {})
    tail = barrier
    void barrier.then(() => {
      if (tail === barrier) tail = undefined
    })
    return result
  }

  function enqueue<T>(write: () => Promise<T>): Promise<T> {
    const run = () => {
      try {
        return Promise.resolve(write())
      } catch (error) {
        return Promise.reject(error)
      }
    }
    const result = tail ? tail.then(run, run) : run()
    return track(result)
  }

  return {
    resume(movieID: number, source: WatchHistoryScope, saved: WatchResume | undefined) {
      const local = pending.get(key(movieID, source))?.resume
      // A deleted/recreated history row or another source must not inherit a hint.
      return local && local.id === (saved?.id ?? 0) ? local : saved
    },
    clear(source: WatchHistoryScope, ids?: number[]) {
      for (const [entryKey, entry] of pending) {
        const [, accountID, directoryID] = JSON.parse(entryKey) as [number, string, string]
        if (
          accountID === source.account_id &&
          directoryID === source.directory_id &&
          (!ids || ids.includes(entry.resume.id))
        )
          pending.delete(entryKey)
      }
    },
    create({
      movieID,
      source,
      resume,
      fileID,
      start,
      write,
      now,
      onError,
      onSaved
    }: {
      movieID: number
      source: WatchHistoryScope
      resume?: WatchResume
      fileID: string
      start: () => Promise<WatchSession>
      write: (id: number, progress: WatchProgress, keepalive: boolean) => Promise<unknown>
      now?: () => number
      onError?: (error: unknown) => void
      onSaved?: () => void
    }) {
      const owner = {}
      const entryKey = key(movieID, source)
      let session: WatchSession | undefined
      let registration: Promise<WatchSession> | undefined

      function begin() {
        if (!registration) {
          registration = enqueue(async () => {
            session = await start()
            const local = pending.get(entryKey)
            if (local && (local.owner === owner || local.resume.id === 0)) {
              local.resume.id = session.id
            }
            return session
          })
          // A recording failure is reported by the caller without blocking playback.
          void registration.catch(() => {})
        }
        return registration
      }

      const writer = createWatchProgressWriter({
        fileID,
        now,
        write: (progress, keepalive) => {
          const ready = begin()
          const send = (current: WatchSession) =>
            write(current.id, { ...progress, session_id: current.session_id }, keepalive)
          return track(session ? send(session) : ready.then(send))
        },
        onUpdate: position => {
          const local = pending.get(entryKey)
          const historyID =
            session?.id ?? (local?.owner === owner ? local.resume.id : resume?.id) ?? 0
          pending.delete(entryKey)
          pending.set(entryKey, {
            owner,
            resume: { ...position, id: historyID, file_id: fileID }
          })
          if (pending.size > 64) pending.delete(pending.keys().next().value!)
        },
        onSaved: position => {
          const local = pending.get(entryKey)
          if (
            local?.owner === owner &&
            local.resume.position === position.position &&
            local.resume.duration === position.duration
          )
            pending.delete(entryKey)
          onSaved?.()
        },
        onError
      })
      return { ...writer, start: begin }
    }
  }
}

export const watchSessions = createWatchSessionQueue()
