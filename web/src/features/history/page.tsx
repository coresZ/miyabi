import { Link } from '@tanstack/react-router'
import { Trash2Icon, XIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import {
  useRemoveWatchHistory,
  useWatchHistory,
  WATCH_HISTORY_PAGE_SIZE
} from '@/api/watch-history'
import { AppPage } from '@/components/app-page'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { ListPagination } from '@/components/list-pagination'
import { MovieGridLayout, MovieGridSkeleton } from '@/components/movie'
import { PageHeader } from '@/components/page-header'
import { Button } from '@/components/ui/button'
import { watchSessions } from '@/features/player/watch-progress-writer'
import { HistoryCard } from './history-card'
import { HistoryClearDialog } from './history-clear-dialog'
import { useHistorySelection } from './use-history-selection'

import { sourceKey } from '@/lib/source'

type HistoryPageProps = { page: number; onPageChange: (page: number) => void }

export function WatchHistoryPage(props: HistoryPageProps) {
  const history = useWatchHistory(props.page)
  const source = history.data?.source
  return <HistoryContent key={`${props.page}:${sourceKey(source)}`} {...props} history={history} />
}

function HistoryContent({
  page,
  onPageChange,
  history
}: HistoryPageProps & {
  history: ReturnType<typeof useWatchHistory>
}) {
  const remove = useRemoveWatchHistory()
  const [clearMode, setClearMode] = useState<'all' | 'selected' | null>(null)
  const items = history.data?.items ?? []
  const source = history.data?.source
  const total = history.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / WATCH_HISTORY_PAGE_SIZE))

  const {
    selecting,
    setSelecting,
    selected,
    selectedIDs,
    allSelected,
    leaveSelection,
    toggleSelect,
    toggleSelectAll
  } = useHistorySelection(items)

  useEffect(() => {
    if (history.isSuccess && !history.isFetching && page > pageCount) onPageChange(pageCount)
  }, [history.isSuccess, history.isFetching, page, pageCount, onPageChange])

  function openClearDialog(mode: 'all' | 'selected') {
    remove.reset()
    setClearMode(mode)
  }

  function clearHistory() {
    if (!source || !clearMode || (clearMode === 'selected' && selectedIDs.length === 0)) return
    remove.mutate(
      clearMode === 'all'
        ? { type: 'all', source }
        : { type: 'selected', source, ids: selectedIDs },
      {
        onSuccess: () => {
          watchSessions.clear(
            { account_id: source.account_id, directory_id: source.directory.id },
            clearMode === 'selected' ? selectedIDs : undefined
          )
          setClearMode(null)
          leaveSelection()
          toast.success(clearMode === 'all' ? '观看历史已清除' : '已清除选中的观看记录')
        }
      }
    )
  }

  return (
    <AppPage>
      <PageHeader title="观看历史" description="回顾最近观看的影片">
        {selecting ? (
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={items.length === 0 || remove.isPending}
              onClick={toggleSelectAll}
            >
              {allSelected ? '取消全选' : '全选本页'}
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={selectedIDs.length === 0 || remove.isPending}
              onClick={() => openClearDialog('selected')}
            >
              <Trash2Icon />
              清除选中{selectedIDs.length > 0 ? ` (${selectedIDs.length})` : ''}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={remove.isPending}
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
              disabled={total === 0}
              onClick={() => setSelecting(true)}
            >
              选择清除
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={total === 0}
              onClick={() => openClearDialog('all')}
            >
              <Trash2Icon />
              清除全部
            </Button>
          </>
        )}
      </PageHeader>

      {history.isPending ? (
        <MovieGridSkeleton />
      ) : history.isError ? (
        <ErrorState
          message="观看历史加载失败"
          onRetry={() => void history.refetch()}
          retrying={history.isFetching}
        />
      ) : items.length === 0 ? (
        <EmptyState
          className="min-h-0 flex-1"
          title={source ? '还没有观看记录' : '挂载媒体目录后查看观看历史'}
          actions={
            !source ? (
              <Button asChild>
                <Link to="/settings">挂载媒体目录</Link>
              </Button>
            ) : undefined
          }
        />
      ) : (
        <MovieGridLayout>
          {items.map(item => (
            <HistoryCard
              key={item.id}
              item={item}
              selecting={selecting}
              selected={selecting && selected.has(item.id)}
              disabled={remove.isPending}
              onSelect={() => toggleSelect(item.id)}
            />
          ))}
        </MovieGridLayout>
      )}

      {pageCount > 1 ? (
        <ListPagination
          page={page}
          totalPages={pageCount}
          hasMore={page < pageCount}
          disabled={history.isFetching || remove.isPending}
          onPageChange={onPageChange}
        />
      ) : null}

      <HistoryClearDialog
        clearMode={clearMode}
        selectedCount={selectedIDs.length}
        isPending={remove.isPending}
        error={remove.error}
        onClose={() => setClearMode(null)}
        onConfirm={clearHistory}
      />
    </AppPage>
  )
}
