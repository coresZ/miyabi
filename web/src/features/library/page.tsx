import { Link } from '@tanstack/react-router'

import { LIBRARY_PAGE_SIZE, useLibraryMovies } from '@/api/library'
import { isScanTask, isTaskActive, useTasks } from '@/api/tasks'
import { AppPage } from '@/components/app-page'
import { EmptyState } from '@/components/empty-state'
import { ErrorState, InlineError } from '@/components/error-state'
import { ListPagination } from '@/components/list-pagination'
import { MovieGridLayout } from '@/components/movie/movie-grid-layout'
import { MovieGridSkeleton } from '@/components/movie/movie-grid-skeleton'
import { PageHeader } from '@/components/page-header'
import { Button } from '@/components/ui/button'
import { LibraryMovieCard } from '@/features/library/movie-card'
import { LibraryScanButton } from '@/features/library/scan-button'
import { useTaskConnection } from '@/features/tasks/task-events'
import { sameSource } from '@/lib/source'

export function LibraryPage({
  page,
  onPageChange
}: {
  page: number
  onPageChange: (page: number) => void
}) {
  const library = useLibraryMovies(page)
  const tasks = useTasks()
  const connection = useTaskConnection()
  const source = library.data?.source
  const sourceTasks = tasks.data?.filter(isScanTask).filter(task => sameSource(task.source, source))
  const latest = sourceTasks?.find(task => !task.movie_id && !task.offline_task_id)
  const scanning = latest !== undefined && isTaskActive(latest)
  const processingMovies = new Set(sourceTasks?.filter(isTaskActive).map(task => task.movie_id))

  return (
    <AppPage>
      <PageHeader title="媒体库" description="来自 115 网盘的影片" inlineActions>
        <LibraryScanButton
          loading={library.isPending}
          available={!!source}
          scanning={scanning}
          rebuilding={!!latest?.rebuild}
          connected={connection.status === 'connected'}
          onStarted={() => onPageChange(1)}
        />
        {library.isSuccess && !source ? (
          <Button asChild>
            <Link to="/settings">挂载媒体目录</Link>
          </Button>
        ) : null}
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

      {library.isPending ? (
        <MovieGridSkeleton count={LIBRARY_PAGE_SIZE} compact />
      ) : library.isError ? (
        <ErrorState
          message="无法读取媒体库，请检查后端服务后重试"
          onRetry={() => void library.refetch()}
          retrying={library.isFetching}
        />
      ) : (
        <>
          {library.data.movies.length > 0 ? (
            <MovieGridLayout>
              {library.data.movies.map(movie => (
                <LibraryMovieCard
                  key={movie.id}
                  movie={movie}
                  busy={!source || scanning || processingMovies.has(movie.id)}
                />
              ))}
            </MovieGridLayout>
          ) : (
            <EmptyState
              className="min-h-0 flex-1 py-12"
              title={
                !source
                  ? '尚未挂载媒体目录'
                  : scanning
                    ? connection.status === 'connected'
                      ? `${latest?.rebuild ? '正在重建' : '正在同步'}，识别到的影片会陆续显示`
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
                    <Button asChild size="lg">
                      <Link to="/settings">去设置挂载目录</Link>
                    </Button>
                  </div>
                ) : !scanning ? (
                  <Button asChild variant="outline">
                    <Link to="/settings">检查媒体目录</Link>
                  </Button>
                ) : null
              }
            />
          )}
          <ListPagination
            page={page}
            totalPages={Math.max(1, Math.ceil(library.data.total / LIBRARY_PAGE_SIZE))}
            hasMore={library.data.has_more}
            disabled={library.isFetching}
            onPageChange={onPageChange}
          />
        </>
      )}
    </AppPage>
  )
}
