import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  diffTaskNotifications,
  scanFingerprint,
  batchFingerprint
} from '../src/features/tasks/task-notification-diff.ts'

const dummySource = {
  account_id: 'acc1',
  directory: { id: 'dir1', name: 'Movies', path: '/Movies' }
}

function makeScanTask(overrides = {}) {
  return {
    id: 1,
    type: 'scan',
    status: 'running',
    progress: 20,
    source: dummySource,
    scan: {
      stage: 'scanning',
      current_path: '/Movies',
      directories_discovered: 10,
      directories_scanned: 2,
      files_scanned: 50,
      video_files: 20,
      matched_files: 15,
      unmatched_files: 5,
      movies: 15,
      removed_files: 0,
      removed_movies: 0,
      metadata_total: 15,
      metadata_completed: 0,
      artwork_total: 0,
      artwork_completed: 0
    },
    ...overrides
  }
}

function makeBatchTask(overrides = {}) {
  return {
    id: 2,
    type: 'subscription_batch',
    status: 'running',
    progress: 50,
    batch: {
      total: 10,
      processed: 5,
      submitted: 5,
      waiting: 0,
      failed: 0
    },
    ...overrides
  }
}

test('fingerprint produces consistent and discriminative strings', () => {
  const t1 = makeScanTask()
  const fp1 = scanFingerprint(t1)
  assert.equal(typeof fp1, 'string')
  assert.equal(fp1, scanFingerprint(t1))

  const t2 = makeScanTask({ progress: 40 })
  assert.notEqual(fp1, scanFingerprint(t2))

  const b1 = makeBatchTask()
  const bfp1 = batchFingerprint(b1)
  assert.equal(typeof bfp1, 'string')
  assert.equal(bfp1, batchFingerprint(b1))

  const b2 = makeBatchTask({ batch: { total: 10, processed: 6, submitted: 6, waiting: 0, failed: 0 } })
  assert.notEqual(bfp1, batchFingerprint(b2))
})

test('diffTaskNotifications announces new active tasks and tracks them', () => {
  const scanTask = makeScanTask()
  const batchTask = makeBatchTask()

  const result = diffTaskNotifications({
    tasks: [scanTask, batchTask],
    activity: { source: dummySource, tasks: [] },
    waiting: false,
    isTasksError: false,
    isActivityError: false,
    previous: new Map(),
    dismissed: new Set(),
    announced: new Set(),
    initialized: false
  })

  assert.equal(result.actions.length, 2)
  assert.equal(result.actions[0].type, 'notify_scan')
  assert.equal(result.actions[1].type, 'notify_batch')
  assert.equal(result.dismissIDs.length, 0)
  assert.equal(result.nextEntries.size, 2)
})

test('diffTaskNotifications avoids re-notifying when fingerprint is unchanged', () => {
  const scanTask = makeScanTask()
  const first = diffTaskNotifications({
    tasks: [scanTask],
    activity: { source: dummySource, tasks: [] },
    waiting: false,
    isTasksError: false,
    isActivityError: false,
    previous: new Map(),
    dismissed: new Set(),
    announced: new Set(),
    initialized: false
  })

  // Second pass with same task and previous entries
  const second = diffTaskNotifications({
    tasks: [scanTask],
    activity: { source: dummySource, tasks: [] },
    waiting: false,
    isTasksError: false,
    isActivityError: false,
    previous: first.nextEntries,
    dismissed: new Set(),
    announced: new Set(),
    initialized: true
  })

  assert.equal(second.actions.length, 0)
  assert.equal(second.dismissIDs.length, 0)
})

test('diffTaskNotifications notifies when task status changes to completed and cleans removed tasks', () => {
  const activeTask = makeScanTask({ status: 'running' })
  const first = diffTaskNotifications({
    tasks: [activeTask],
    activity: { source: dummySource, tasks: [] },
    waiting: false,
    isTasksError: false,
    isActivityError: false,
    previous: new Map(),
    dismissed: new Set(),
    announced: new Set(),
    initialized: false
  })

  // Now task completed
  const completedTask = makeScanTask({ status: 'done', progress: 100 })
  const second = diffTaskNotifications({
    tasks: [completedTask],
    activity: { source: dummySource, tasks: [] },
    waiting: false,
    isTasksError: false,
    isActivityError: false,
    previous: first.nextEntries,
    dismissed: new Set(),
    announced: new Set(),
    initialized: true
  })

  assert.equal(second.actions.length, 1)
  assert.equal(second.actions[0].type, 'notify_scan')

  // Third pass: task is removed from tasks list
  const third = diffTaskNotifications({
    tasks: [],
    activity: { source: dummySource, tasks: [] },
    waiting: false,
    isTasksError: false,
    isActivityError: false,
    previous: second.nextEntries,
    dismissed: new Set(),
    announced: new Set(),
    initialized: true
  })

  assert.equal(third.actions.length, 0)
  assert.equal(third.dismissIDs.length, 1)
  assert.equal(third.nextEntries.size, 0)
})
