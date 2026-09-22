import { useState } from 'react'

import { useActorFeed, useSubscriptions } from '@/api/subscriptions'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ActorChip } from './actor-chip'
import { MovieSubscriptions } from './movie-subscriptions'

// Subscribed actors above the grid of works they spawned. Selecting an actor
// narrows the grid to that actor's feed.
export function ActorSubscriptions() {
  const actors = useSubscriptions('actor')
  const movies = useSubscriptions('movie')
  const [selectedID, setSelectedID] = useState<number | null>(null)
  const feed = useActorFeed(selectedID)

  if (actors.isPending) {
    return (
      <div className="flex gap-3 overflow-hidden">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton key={index} className="h-16 w-64 shrink-0 rounded-2xl" />
        ))}
      </div>
    )
  }
  if (actors.isError) {
    return (
      <ErrorState
        message="演员订阅加载失败"
        onRetry={() => void actors.refetch()}
        retrying={actors.isFetching}
      />
    )
  }
  if (actors.data.length === 0) {
    return <EmptyState title="还没有订阅演员，在演员作品页点击「订阅演员」" />
  }

  const spawned = (movies.data ?? []).filter(item => item.origin_id != null)
  const counts = new Map<number, number>()
  for (const item of spawned) {
    counts.set(item.origin_id ?? 0, (counts.get(item.origin_id ?? 0) ?? 0) + 1)
  }
  const grid =
    selectedID === null
      ? { items: spawned, query: movies }
      : { items: feed.data ?? [], query: feed }

  return (
    <div className="space-y-6">
      <div className="flex gap-3 overflow-x-auto pb-2">
        <Button
          type="button"
          variant={selectedID === null ? 'secondary' : 'outline'}
          className="h-auto shrink-0 rounded-2xl px-4 py-3"
          onClick={() => setSelectedID(null)}
        >
          全部新作
          <Badge variant="outline">{spawned.length}</Badge>
        </Button>
        {actors.data.map(actor => (
          <ActorChip
            key={actor.id}
            actor={actor}
            count={counts.get(actor.id) ?? 0}
            selected={selectedID === actor.id}
            onSelect={() => setSelectedID(selectedID === actor.id ? null : actor.id)}
            onRemoved={() => {
              if (selectedID === actor.id) setSelectedID(null)
            }}
          />
        ))}
      </div>

      <MovieSubscriptions
        items={grid.items}
        isPending={grid.query.isPending}
        isError={grid.query.isError}
        isFetching={grid.query.isFetching}
        onRetry={() => void grid.query.refetch()}
        emptyTitle={selectedID === null ? '订阅的演员暂时没有新作' : '这位演员暂时没有新作'}
      />
    </div>
  )
}
