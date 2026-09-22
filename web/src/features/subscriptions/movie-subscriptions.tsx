import { CloudDownloadIcon, LoaderCircleIcon, XIcon } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { type SubscriptionItem, useBatchEnqueueSubscriptions } from '@/api/subscriptions'
import { EmptyState } from '@/components/empty-state'
import { ErrorState, InlineError } from '@/components/error-state'
import { MovieGridLayout, MovieGridSkeleton } from '@/components/movie'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { isPendingSubscription, SubscriptionCard } from './subscription-card'

type MovieSubscriptionsProps = {
  items: SubscriptionItem[]
  isPending: boolean
  isError: boolean
  isFetching: boolean
  onRetry: () => void
  emptyTitle: string
}

// Movie subscription grid with the history page's selection mode: pick some
// cards, or ingest every pending one, through a single queued batch task.
export function MovieSubscriptions({
  items,
  isPending,
  isError,
  isFetching,
  onRetry,
  emptyTitle
}: MovieSubscriptionsProps) {
  const batch = useBatchEnqueueSubscriptions()
  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(() => new Set())
  const [confirmAll, setConfirmAll] = useState(false)
  const pending = items.filter(isPendingSubscription)
  const selectedIDs = pending.filter(item => selected.has(item.id)).map(item => item.id)
  const allSelected = pending.length > 0 && selectedIDs.length === pending.length

  function leaveSelection() {
    setSelecting(false)
    setSelected(new Set())
  }

  function enqueue(payload: { ids?: number[]; all?: boolean }, count: number) {
    batch.mutate(payload, {
      onSuccess: () => {
        setConfirmAll(false)
        leaveSelection()
        toast.success(`已创建 ${count} 部影片的入库任务`, {
          description: '按磁力偏好逐部加入 115，进度见任务通知。'
        })
      }
    })
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-2">
        {selecting ? (
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={pending.length === 0 || batch.isPending}
              onClick={() =>
                setSelected(allSelected ? new Set() : new Set(pending.map(item => item.id)))
              }
            >
              {allSelected ? '取消全选' : '全选待入库'}
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={selectedIDs.length === 0 || batch.isPending}
              onClick={() => enqueue({ ids: selectedIDs }, selectedIDs.length)}
            >
              {batch.isPending ? (
                <LoaderCircleIcon className="animate-spin" />
              ) : (
                <CloudDownloadIcon />
              )}
              入库选中{selectedIDs.length > 0 ? ` (${selectedIDs.length})` : ''}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={batch.isPending}
              onClick={leaveSelection}
            >
              <XIcon />
              退出
            </Button>
          </>
        ) : (
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={pending.length === 0}
              onClick={() => setSelecting(true)}
            >
              选择入库
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={pending.length === 0}
              onClick={() => setConfirmAll(true)}
            >
              <CloudDownloadIcon />
              一键入库{pending.length > 0 ? ` (${pending.length})` : ''}
            </Button>
          </>
        )}
      </div>

      {isPending ? (
        <MovieGridSkeleton />
      ) : isError ? (
        <ErrorState message="订阅列表加载失败" onRetry={onRetry} retrying={isFetching} />
      ) : items.length === 0 ? (
        <EmptyState className="min-h-0 flex-1" title={emptyTitle} />
      ) : (
        <MovieGridLayout>
          {items.map(item => (
            <SubscriptionCard
              key={item.id}
              item={item}
              selecting={selecting}
              selected={selecting && selected.has(item.id)}
              disabled={batch.isPending || (selecting && !isPendingSubscription(item))}
              onSelect={() =>
                setSelected(current => {
                  const next = new Set(current)
                  if (next.has(item.id)) next.delete(item.id)
                  else next.add(item.id)
                  return next
                })
              }
            />
          ))}
        </MovieGridLayout>
      )}

      <Dialog
        open={confirmAll}
        onOpenChange={open => {
          if (!open && !batch.isPending) setConfirmAll(false)
        }}
      >
        <DialogContent showCloseButton={!batch.isPending}>
          <DialogHeader>
            <DialogTitle>一键入库</DialogTitle>
            <DialogDescription>
              为 {pending.length} 部等待磁力的影片创建入库任务？任务按磁力偏好逐部加入 115，
              每部之间随机间隔 1.5 到 3 秒；没有合适磁力的影片保持等待并开启自动入库。
            </DialogDescription>
          </DialogHeader>
          {batch.error ? <InlineError>{batch.error.message}</InlineError> : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={batch.isPending}
              onClick={() => setConfirmAll(false)}
            >
              取消
            </Button>
            <Button
              type="button"
              disabled={batch.isPending || pending.length === 0}
              onClick={() => enqueue({ all: true }, pending.length)}
            >
              {batch.isPending ? (
                <LoaderCircleIcon className="animate-spin" />
              ) : (
                <CloudDownloadIcon />
              )}
              确认入库
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
