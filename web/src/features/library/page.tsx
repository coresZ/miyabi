import { useCallback, useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'

import { useLibraryMovies } from '@/api/library'
import { isTaskActive } from '@/api/tasks'
import { AppPage } from '@/components/app-page'
import { EmptyState } from '@/components/empty-state'
import { ErrorState, InlineError } from '@/components/error-state'
import { PageHeader } from '@/components/page-header'
import { Button } from '@/components/ui/button'
import { ContinueWatching } from '@/features/library/continue-watching'
import { LibraryProgress } from '@/features/library/library-progress'
import { LazyLibraryPlayerDialog } from '@/features/library/player-dialog-lazy'
import { RecentlyAddedShelf } from '@/features/library/recent-shelf'
import { TopRatedShelf } from '@/features/library/top-rated-shelf'
import { isDesktopShell } from '@/features/settings/desktop-bridge'
import { useTaskConnection } from '@/features/tasks/task-events'
import { useActiveScan } from '@/features/tasks/use-active-scan'

export function LibraryPage() {
  const library = useLibraryMovies(1)
  // `active` covers any active library task, including a single-movie rescrape.
  const { tasks, scans, active: progressTask } = useActiveScan()
  const connection = useTaskConnection()
  const navigate = useNavigate()
  const [playingId, setPlayingId] = useState<number | null>(null)
  const source = library.data?.source
  const latestScan = scans?.find(task => !task.movie_id && !task.offline_task_id)
  const scanning = latestScan !== undefined && isTaskActive(latestScan)

  // The desktop shell plays on the immersive /play page; the browser keeps the
  // dialog player so the library list stays behind it.
  const openPlayer = useCallback(
    (id: number) => {
      if (isDesktopShell()) {
        void navigate({ to: '/play', search: { movie: id } })
        return
      }
      setPlayingId(id)
    },
    [navigate]
  )

  return (
    <AppPage>
      <PageHeader title="媒体库" description="最近加入、正在观看与高分影片" inlineActions>
        <LibraryProgress task={progressTask} />
      </PageHeader>

      {tasks.isError ? (
        <InlineError
          onRetry={connection.reconnect}
          retrying={connection.status === 'connecting'}
          retryLabel="重新连接"
        >
          暂时无法获取任务状态，请检查后端服务后重试。
        </InlineError>
      ) : null}

      {library.isError ? (
        <ErrorState
          message="无法读取媒体库，请检查后端服务后重试"
          onRetry={() => void library.refetch()}
          retrying={library.isFetching}
        />
      ) : (
        <>
          <ContinueWatching onPlay={openPlayer} />
          <RecentlyAddedShelf onPlay={openPlayer} />
          <TopRatedShelf onPlay={openPlayer} />
          {!library.isPending && library.data.total === 0 ? (
            <EmptyState
              className="min-h-0 flex-1 py-12"
              title={
                !source
                  ? '尚未挂载媒体目录'
                  : scanning
                    ? connection.status === 'connected'
                      ? `${latestScan?.rebuild ? '正在重建' : '正在同步'}，识别到的影片会陆续显示`
                      : '连接中，等待更新媒体库进度'
                    : '未识别到影片'
              }
              description={
                !source
                  ? '完成下方步骤后即可同步 115 影片到媒体库'
                  : scanning
                    ? undefined
                    : '可尝试重新扫描，或确认目录中包含可识别的影片文件'
              }
              actions={
                !source ? (
                  <div className="flex max-w-md flex-col items-center gap-4">
                    <ol className="w-full space-y-2 rounded-2xl border bg-card/60 px-4 py-3 text-left text-sm text-muted-foreground">
                      <li>
                        <span className="font-medium text-foreground">1.</span> 在设置中登录 115
                        网盘
                      </li>
                      <li>
                        <span className="font-medium text-foreground">2.</span> 选择媒体库目录
                      </li>
                      <li>
                        <span className="font-medium text-foreground">3.</span> 返回此处扫描入库
                      </li>
                    </ol>
                    <Button size="lg" nativeButton={false} render={<Link to="/settings" />}>
                      去设置挂载目录
                    </Button>
                  </div>
                ) : !scanning ? (
                  <Button variant="outline" nativeButton={false} render={<Link to="/settings" />}>
                    检查媒体目录
                  </Button>
                ) : null
              }
            />
          ) : null}
        </>
      )}

      <LazyLibraryPlayerDialog movieId={playingId} onClose={() => setPlayingId(null)} />
    </AppPage>
  )
}
