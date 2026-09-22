import assert from 'node:assert/strict'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'

import { createWatchSessionQueue } from '../src/features/player/watch-progress-writer.ts'
import { watchResumePosition } from '../src/lib/watch-progress.ts'

const source = { account_id: 'account', directory_id: 'directory' }
const saved = { id: 7, file_id: 'second-video', position: 120, duration: 600 }

function recorder(queue, options = {}) {
  return queue.create({
    movieID: 1,
    source,
    resume: saved,
    fileID: saved.file_id,
    now: () => 0,
    start: async () => ({ ...saved, session_id: 'new-session' }),
    write: async () => {},
    ...options
  })
}

test('slow watch creation keeps resume readable and buffers progress until the session is ready', async () => {
  const queue = createWatchSessionQueue()
  const opening = Promise.withResolvers()
  const requests = []
  const player = recorder(queue, {
    start: () => opening.promise,
    write: async (id, progress, keepalive) => requests.push({ id, progress, keepalive })
  })
  const starting = player.start()
  assert.equal(player.start(), starting, 'effect replay must not create a second session')
  const resume = queue.resume(1, source, saved)
  assert.equal(resume.file_id, 'second-video')
  assert.equal(watchResumePosition(resume, 'second-video'), 120)
  player.update(135, 600)
  const closing = player.flush(true)
  await setImmediate()
  assert.equal(requests.length, 0)
  assert.equal(queue.resume(1, source, saved).position, 135)

  opening.resolve({ ...saved, session_id: 'created-session', file_id: 'first-video', position: 0 })
  await closing
  assert.equal(
    resume.position,
    120,
    'late registration must not mutate the initial resume snapshot'
  )
  assert.deepEqual(requests, [
    {
      id: 7,
      progress: {
        file_id: 'second-video',
        session_id: 'created-session',
        position: 135,
        duration: 600,
        version: 1
      },
      keepalive: true
    }
  ])
})

test('closing progress is saved before a rapid reopening creates its new session', async () => {
  const queue = createWatchSessionQueue()
  const firstOpening = Promise.withResolvers()
  const finalWrite = Promise.withResolvers()
  const secondOpening = Promise.withResolvers()
  const events = []
  const first = recorder(queue, {
    start: () => {
      events.push('open:first')
      return firstOpening.promise
    },
    write: (_id, progress) => {
      events.push(`save:${progress.session_id}:${progress.position}`)
      return finalWrite.promise
    }
  })
  void first.start()
  first.update(150, 600)
  const closing = first.flush(true)
  const resume = queue.resume(1, source, saved)
  assert.equal(
    resume.position,
    150,
    'reopening must see the final position before its write completes'
  )
  const second = recorder(queue, {
    resume,
    start: () => {
      events.push('open:second')
      return secondOpening.promise
    },
    write: async (_id, progress) => events.push(`save:${progress.session_id}:${progress.position}`)
  })
  void second.start()
  second.update(165, 600)
  const nextFlush = second.flush(true)
  firstOpening.resolve({ ...saved, session_id: 'first' })
  await setImmediate()
  assert.deepEqual(events, ['open:first', 'save:first:150'])
  finalWrite.resolve()
  await closing
  await setImmediate()
  assert.deepEqual(events, ['open:first', 'save:first:150', 'open:second'])
  assert.equal(
    queue.resume(1, source, saved).position,
    165,
    'the previous save must not discard newer local progress'
  )
  secondOpening.resolve({ ...saved, session_id: 'second' })
  await nextFlush
  assert.deepEqual(events, ['open:first', 'save:first:150', 'open:second', 'save:second:165'])
})

test('a failed watch write does not prevent another opening from recording', async () => {
  const queue = createWatchSessionQueue()
  const failure = new Error('watch write failed')
  const errors = []
  const failed = recorder(queue, {
    start: async () => {
      throw failure
    },
    write: async () => assert.fail('a failed registration has no writable session'),
    onError: error => errors.push(error)
  })
  await assert.rejects(failed.start(), failure)
  failed.update(130, 600)
  await failed.flush(true)
  assert.deepEqual(errors, [failure])
  assert.equal(queue.resume(1, source, saved).position, 130)

  const writes = []
  const next = recorder(queue, { write: async (_id, progress) => writes.push(progress) })
  await next.start()
  next.update(140, 600)
  await next.flush(true)
  assert.equal(writes[0].position, 140)
  assert.equal(writes[0].session_id, 'new-session')
})

test('rapid reopenings retain progress when the first history entry is still being created', async () => {
  const queue = createWatchSessionQueue()
  const opening = Promise.withResolvers()
  const saving = Promise.withResolvers()
  const first = recorder(queue, {
    resume: undefined,
    start: () => opening.promise,
    write: () => saving.promise
  })
  void first.start()
  first.update(10, 600)
  const closing = first.flush(true)
  const second = recorder(queue, {
    resume: queue.resume(1, source, undefined),
    start: async () => ({ ...saved, id: 9, session_id: 'second' })
  })
  const reopening = second.start()
  second.update(20, 600)
  opening.resolve({ ...saved, id: 9, session_id: 'first' })
  await setImmediate()
  const serverResume = { ...saved, id: 9, position: 0 }
  assert.equal(queue.resume(1, source, serverResume).position, 20)
  second.update(21, 600)
  assert.equal(queue.resume(1, source, serverResume).position, 21)
  saving.resolve()
  await closing
  await reopening
  await second.flush(true)
})

test('pending resume is isolated by source, movie and history identity, and clearing history removes it', () => {
  const queue = createWatchSessionQueue()
  const player = recorder(queue)
  player.update(150, 600)
  assert.equal(queue.resume(1, source, saved).position, 150)
  assert.equal(queue.resume(2, source, saved), saved)
  assert.equal(queue.resume(1, { ...source, account_id: 'other' }, saved), saved)
  assert.equal(queue.resume(1, { ...source, directory_id: 'other' }, saved), saved)
  assert.equal(queue.resume(1, source, undefined), undefined)
  const recreated = { ...saved, id: 8, position: 0 }
  assert.equal(queue.resume(1, source, recreated), recreated)
  queue.clear(source, [8])
  assert.equal(queue.resume(1, source, saved).position, 150)
  queue.clear(source, [7])
  assert.equal(queue.resume(1, source, saved), saved)
})

test('an idle established session dispatches keepalive synchronously and retains newer unsaved progress', async () => {
  const queue = createWatchSessionQueue()
  const response = Promise.withResolvers()
  const requests = []
  const player = recorder(queue, {
    write: (_id, progress, keepalive) => {
      requests.push({ progress, keepalive })
      return response.promise
    }
  })
  await player.start()
  player.update(150, 600)
  const flushing = player.flush(true)
  assert.equal(requests.length, 1, 'pagehide must initiate the keepalive request immediately')
  assert.equal(requests[0].keepalive, true)
  player.update(160, 600)
  response.resolve()
  await flushing
  assert.equal(queue.resume(1, source, saved).position, 160)
})

test('pagehide sends final progress while an older write is pending and reopening waits for both', async () => {
  const queue = createWatchSessionQueue()
  let now = 0
  const requests = []
  const player = recorder(queue, {
    now: () => now,
    write: (_id, progress, keepalive) => {
      const response = Promise.withResolvers()
      requests.push({ progress, keepalive, ...response })
      return response.promise
    }
  })
  await player.start()
  now = 10_000
  player.update(130, 600)
  assert.equal(requests.length, 1)
  player.update(140, 600)
  const closing = player.flush(true)
  assert.equal(requests.length, 2, 'a slow regular write must not delay pagehide keepalive')
  assert.equal(requests[1].keepalive, true)
  assert.equal(requests[1].progress.version, 2)
  let reopened = false
  const next = recorder(queue, {
    start: async () => {
      reopened = true
      return { ...saved, session_id: 'next' }
    }
  })
  const opening = next.start()
  requests[1].resolve()
  await closing
  await setImmediate()
  assert.equal(reopened, false)
  requests[0].resolve()
  await opening
  assert.equal(reopened, true)
})
