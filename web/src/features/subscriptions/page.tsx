import { useState } from 'react'

import { useSubscriptions } from '@/api/subscriptions'
import { AppPage } from '@/components/app-page'
import { PageHeader } from '@/components/page-header'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ActorSubscriptions } from './actor-subscriptions'
import { MovieSubscriptions } from './movie-subscriptions'

type SubscriptionsView = 'movies' | 'actors'

export function SubscriptionsPage() {
  const [view, setView] = useState<SubscriptionsView>('movies')
  const movies = useSubscriptions('movie', view === 'movies')

  return (
    <AppPage>
      <PageHeader title="订阅" description="追踪影片磁力与演员新作，出现资源后加入 115" />
      <div className="min-w-0 space-y-6">
        <Tabs value={view} onValueChange={value => setView(value as SubscriptionsView)}>
          <TabsList>
            <TabsTrigger value="movies">影片订阅</TabsTrigger>
            <TabsTrigger value="actors">演员订阅</TabsTrigger>
          </TabsList>
        </Tabs>

        {view === 'actors' ? (
          <ActorSubscriptions />
        ) : (
          <MovieSubscriptions
            items={movies.data ?? []}
            isPending={movies.isPending}
            isError={movies.isError}
            isFetching={movies.isFetching}
            onRetry={() => void movies.refetch()}
            emptyTitle="还没有订阅影片，在「即将发行」卡片右上角或详情页点击铃铛订阅"
          />
        )}
      </div>
    </AppPage>
  )
}
