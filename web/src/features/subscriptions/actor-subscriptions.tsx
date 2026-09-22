import { PauseIcon, PlayIcon, Trash2Icon, UserIcon } from 'lucide-react'
import { useState } from 'react'

import {
  useActorFeed,
  useRemoveSubscription,
  useSubscriptions,
  useUpdateSubscription,
  type SubscriptionItem
} from '@/api/subscriptions'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { MovieSubscriptions } from './movie-subscriptions'

export function ActorSubscriptions() {
  const actorsQuery = useSubscriptions('actor')
  const allMoviesQuery = useSubscriptions('movie')
  const [selectedActorID, setSelectedActorID] = useState<number | null>(null)

  const feedQuery = useActorFeed(selectedActorID ?? 0, selectedActorID !== null)
  const update = useUpdateSubscription()
  const remove = useRemoveSubscription()

  if (actorsQuery.isPending) {
    return <div className="h-48 animate-pulse rounded-2xl bg-muted/40" />
  }
  if (actorsQuery.isError) {
    return <ErrorState message="演员订阅列表加载失败" onRetry={() => void actorsQuery.refetch()} />
  }

  const actors = actorsQuery.data ?? []

  if (actors.length === 0) {
    return <EmptyState title="暂无演员订阅，在演员作品页点击「订阅」按钮即可订阅" />
  }

  // Calculate releases count per actor
  const allMovies = allMoviesQuery.data ?? []
  const actorMovieCounts = new Map<number, number>()
  for (const m of allMovies) {
    if (m.origin_id) {
      actorMovieCounts.set(m.origin_id, (actorMovieCounts.get(m.origin_id) ?? 0) + 1)
    }
  }

  // Determine which movies to display in the grid
  let displayedMovies: SubscriptionItem[]
  let isGridLoading = false
  let isGridError = false
  let onGridRetry: () => void = () => {}

  if (selectedActorID !== null) {
    displayedMovies = feedQuery.data ?? []
    isGridLoading = feedQuery.isPending
    isGridError = feedQuery.isError
    onGridRetry = () => void feedQuery.refetch()
  } else {
    // Show all actor-spawned releases (or all releases with origin_id)
    displayedMovies = allMovies.filter(m => m.origin_id != null)
    if (displayedMovies.length === 0) {
      // If none have origin_id, show all movie subscriptions
      displayedMovies = allMovies
    }
    isGridLoading = allMoviesQuery.isPending
    isGridError = allMoviesQuery.isError
    onGridRetry = () => void allMoviesQuery.refetch()
  }

  return (
    <div className="space-y-6">
      {/* Horizontal scrollable actors list */}
      <div className="space-y-2">
        <div className="text-xs font-medium text-muted-foreground">
          已订阅演员 ({actors.length})
        </div>
        <div className="flex scrollbar-thin gap-2 overflow-x-auto pb-2">
          <Button
            type="button"
            variant={selectedActorID === null ? 'default' : 'outline'}
            size="sm"
            className="h-14 shrink-0 rounded-2xl px-4"
            onClick={() => setSelectedActorID(null)}
          >
            全部新作
            <Badge variant={selectedActorID === null ? 'outline' : 'secondary'} className="ml-1.5">
              {displayedMovies.length}
            </Badge>
          </Button>

          {actors.map(actor => {
            const isSelected = selectedActorID === actor.id
            const count = actorMovieCounts.get(actor.id) ?? 0
            const isPaused = actor.status === 'paused'

            return (
              <div
                key={actor.id}
                className={`group relative flex shrink-0 cursor-pointer items-center gap-2.5 rounded-2xl border p-2 transition-all ${
                  isSelected
                    ? 'border-primary bg-primary/10 ring-1 ring-primary'
                    : 'border-border/60 bg-card/60 hover:bg-card/90'
                }`}
                onClick={() => setSelectedActorID(actor.id)}
              >
                <Avatar size="default" className="size-10">
                  <AvatarImage src={actor.cover} alt={actor.title} />
                  <AvatarFallback>{actor.title.slice(0, 1) || <UserIcon />}</AvatarFallback>
                </Avatar>

                <div className="min-w-0 pr-1 text-left">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate text-sm font-medium">{actor.title}</span>
                    {isPaused ? (
                      <Badge variant="secondary" className="px-1 py-0 text-[10px]">
                        已暂停
                      </Badge>
                    ) : null}
                  </div>
                  <div className="text-xs text-muted-foreground">新作 {count} 部</div>
                </div>

                {/* Inline controls */}
                <div
                  className="flex items-center gap-1 pl-1"
                  onClick={event => event.stopPropagation()}
                >
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="flex items-center">
                        <Switch
                          size="sm"
                          checked={actor.auto_download}
                          onCheckedChange={checked =>
                            update.mutate({ id: actor.id, auto_download: checked })
                          }
                        />
                      </div>
                    </TooltipTrigger>
                    <TooltipContent side="top">
                      {actor.auto_download ? '新作自动推送已开启' : '新作自动推送已关闭'}
                    </TooltipContent>
                  </Tooltip>

                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        className="size-7"
                        onClick={() =>
                          update.mutate({
                            id: actor.id,
                            status: isPaused ? 'active' : 'paused'
                          })
                        }
                      >
                        {isPaused ? (
                          <PlayIcon className="size-3.5" />
                        ) : (
                          <PauseIcon className="size-3.5" />
                        )}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent side="top">{isPaused ? '恢复检查' : '暂停检查'}</TooltipContent>
                  </Tooltip>

                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        className="size-7 text-muted-foreground hover:text-destructive"
                        onClick={() => {
                          if (selectedActorID === actor.id) {
                            setSelectedActorID(null)
                          }
                          remove.mutate(actor.id)
                        }}
                      >
                        <Trash2Icon className="size-3.5" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent side="top">取消订阅</TooltipContent>
                  </Tooltip>
                </div>
              </div>
            )
          })}
        </div>
      </div>

      {/* Movies Grid for selected actor or all actor releases */}
      <MovieSubscriptions
        items={displayedMovies}
        isLoading={isGridLoading}
        isError={isGridError}
        onRetry={onGridRetry}
      />
    </div>
  )
}
