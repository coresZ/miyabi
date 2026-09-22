import { Link } from '@tanstack/react-router'
import {
  CheckSquareIcon,
  DownloadCloudIcon,
  LoaderCircleIcon,
  SquareIcon,
  Trash2Icon,
  XIcon
} from 'lucide-react'
import { useState } from 'react'

import {
  useBatchEnqueueSubscriptions,
  useEnqueueSubscription,
  useRemoveSubscription,
  useSubscriptions,
  type SubscriptionItem
} from '@/api/subscriptions'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { MovieCard, MovieGridLayout, MovieGridSkeleton } from '@/components/movie'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'

const statusLabels: Record<SubscriptionItem['status'], string> = {
  waiting: '等待磁力',
  added: '已加入 115',
  stale: '长期无源',
  active: '生效中',
  paused: '已暂停',
  error: '异常'
}

function statusVariant(status: SubscriptionItem['status']) {
  if (status === 'added') return 'success' as const
  if (status === 'stale') return 'secondary' as const
  if (status === 'error') return 'destructive' as const
  return 'default' as const
}

type MovieSubscriptionsProps = {
  items?: SubscriptionItem[]
  isLoading?: boolean
  isError?: boolean
  onRetry?: () => void
}

export function MovieSubscriptions({
  items: propItems,
  isLoading: propLoading,
  isError: propError,
  onRetry: propOnRetry
}: MovieSubscriptionsProps = {}) {
  const query = useSubscriptions('movie', propItems === undefined)
  const items = propItems ?? query.data ?? []
  const isLoading = propLoading ?? query.isPending
  const isError = propError ?? query.isError
  const onRetry = propOnRetry ?? (() => void query.refetch())

  const enqueue = useEnqueueSubscription()
  const remove = useRemoveSubscription()
  const batchEnqueue = useBatchEnqueueSubscriptions()

  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(() => new Set())
  const [dialogOpen, setDialogOpen] = useState(false)

  if (isLoading) return <MovieGridSkeleton />
  if (isError) {
    return <ErrorState message="影片订阅加载失败" onRetry={onRetry} />
  }
  if (items.length === 0) {
    return <EmptyState title="暂无影片订阅，在影片卡片右上角或详情页点击「订阅」即可订阅" />
  }

  const waitingItems = items.filter(item => item.status === 'waiting' || item.status === 'stale')
  const selectedIDs = items.filter(item => selected.has(item.id)).map(item => item.id)
  const allSelected = items.length > 0 && selectedIDs.length === items.length

  function toggleSelect(id: number) {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function handleBatchSelected() {
    if (selectedIDs.length === 0) return
    batchEnqueue.mutate(
      { ids: selectedIDs },
      {
        onSuccess: () => {
          setSelecting(false)
          setSelected(new Set())
        }
      }
    )
  }

  function handleBatchAll() {
    batchEnqueue.mutate(
      { all: true },
      {
        onSuccess: () => {
          setDialogOpen(false)
          setSelecting(false)
          setSelected(new Set())
        }
      }
    )
  }

  return (
    <div className="space-y-4">
      {/* Selection toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border/60 bg-card/60 p-3 backdrop-blur">
        <div className="flex items-center gap-2">
          {selecting ? (
            <>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  if (allSelected) {
                    setSelected(new Set())
                  } else {
                    setSelected(new Set(items.map(item => item.id)))
                  }
                }}
              >
                {allSelected ? <SquareIcon /> : <CheckSquareIcon />}
                {allSelected ? '取消全选' : '全选'}
              </Button>
              <Button
                type="button"
                variant="default"
                size="sm"
                disabled={selectedIDs.length === 0 || batchEnqueue.isPending}
                onClick={handleBatchSelected}
              >
                {batchEnqueue.isPending ? (
                  <LoaderCircleIcon className="animate-spin" />
                ) : (
                  <DownloadCloudIcon />
                )}
                入库选中 ({selectedIDs.length})
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  setSelecting(false)
                  setSelected(new Set())
                }}
              >
                <XIcon />
                取消
              </Button>
            </>
          ) : (
            <>
              <Button type="button" variant="outline" size="sm" onClick={() => setSelecting(true)}>
                批量选择
              </Button>
              {waitingItems.length > 0 ? (
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  disabled={batchEnqueue.isPending}
                  onClick={() => setDialogOpen(true)}
                >
                  <DownloadCloudIcon />
                  一键入库 ({waitingItems.length})
                </Button>
              ) : null}
            </>
          )}
        </div>
        <div className="text-xs text-muted-foreground">
          共 {items.length} 部订阅，{waitingItems.length} 部待入库
        </div>
      </div>

      {/* Grid */}
      <MovieGridLayout>
        {items.map(item => {
          const isSelected = selected.has(item.id)
          const isWaiting = item.status === 'waiting' || item.status === 'stale'
          return (
            <div
              key={item.id}
              className={`relative h-full min-w-0 transition-opacity ${
                selecting && !isSelected ? 'opacity-85' : ''
              }`}
              onClick={
                selecting
                  ? event => {
                      event.preventDefault()
                      toggleSelect(item.id)
                    }
                  : undefined
              }
            >
              <Link
                to="/discover/$movieId"
                params={{ movieId: item.target_id }}
                className="block h-full rounded-2xl outline-ring"
                onClick={selecting ? event => event.preventDefault() : undefined}
              >
                <MovieCard
                  movie={item}
                  description={
                    <div className="space-y-1">
                      <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
                        <span>{item.release_date ? `发行 ${item.release_date}` : '未定档'}</span>
                        {item.auto_download ? (
                          <span className="text-[11px] text-primary">自动推送</span>
                        ) : null}
                      </div>
                      {item.error ? (
                        <p className="line-clamp-2 text-destructive" title={item.error}>
                          {item.error}
                        </p>
                      ) : null}
                    </div>
                  }
                  state={
                    <Badge variant={statusVariant(item.status)}>
                      {statusLabels[item.status] ?? item.status}
                    </Badge>
                  }
                  coverOverlay={
                    selecting ? (
                      <div className="absolute top-2 right-2">
                        <Button
                          type="button"
                          variant={isSelected ? 'default' : 'outline'}
                          size="icon-sm"
                          className="size-7 bg-background/90 shadow backdrop-blur"
                        >
                          {isSelected ? <CheckSquareIcon /> : <SquareIcon />}
                        </Button>
                      </div>
                    ) : undefined
                  }
                >
                  {!selecting ? (
                    <div className="flex w-full items-center justify-end gap-1.5 pt-1">
                      {isWaiting ? (
                        <Button
                          type="button"
                          variant="outline"
                          size="xs"
                          disabled={enqueue.isPending}
                          onClick={event => {
                            event.preventDefault()
                            event.stopPropagation()
                            enqueue.mutate(item.id)
                          }}
                        >
                          {enqueue.isPending && enqueue.variables === item.id ? (
                            <LoaderCircleIcon className="animate-spin" />
                          ) : (
                            <DownloadCloudIcon />
                          )}
                          入库
                        </Button>
                      ) : null}
                      <Button
                        type="button"
                        variant="ghost"
                        size="xs"
                        disabled={remove.isPending}
                        onClick={event => {
                          event.preventDefault()
                          event.stopPropagation()
                          remove.mutate(item.id)
                        }}
                      >
                        {remove.isPending && remove.variables === item.id ? (
                          <LoaderCircleIcon className="animate-spin" />
                        ) : (
                          <Trash2Icon />
                        )}
                        移除
                      </Button>
                    </div>
                  ) : null}
                </MovieCard>
              </Link>
            </div>
          )
        })}
      </MovieGridLayout>

      {/* Confirmation Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>一键入库确认</DialogTitle>
            <DialogDescription>
              将为当前所有 {waitingItems.length} 部待入库影片创建批量入库队列任务。
              系统将按照您的磁力偏好选择最优磁力链，并在每部之间随机间隔 1.5 到 3 秒以保障 115
              账号安全。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="gap-2 sm:gap-0">
            <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
              取消
            </Button>
            <Button
              type="button"
              variant="default"
              disabled={batchEnqueue.isPending}
              onClick={handleBatchAll}
            >
              {batchEnqueue.isPending ? <LoaderCircleIcon className="animate-spin" /> : null}
              确认入库 ({waitingItems.length} 部)
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
