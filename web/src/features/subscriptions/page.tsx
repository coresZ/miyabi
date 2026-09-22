import { useState } from 'react'

import { AppPage } from '@/components/app-page'
import { PageHeader } from '@/components/page-header'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ActorSubscriptions } from './actor-subscriptions'
import { MovieSubscriptions } from './movie-subscriptions'

export function SubscriptionsPage() {
  const [tab, setTab] = useState<'movies' | 'actors'>('movies')

  return (
    <AppPage>
      <PageHeader
        title="订阅"
        description="管理影片追踪与演员新作订阅，发现新磁力后安全批量入库到 115"
      />

      <div className="space-y-6">
        <Tabs value={tab} onValueChange={value => setTab(value as 'movies' | 'actors')}>
          <TabsList>
            <TabsTrigger value="movies">影片订阅</TabsTrigger>
            <TabsTrigger value="actors">演员订阅</TabsTrigger>
          </TabsList>

          <TabsContent value="movies" className="pt-4">
            <MovieSubscriptions />
          </TabsContent>

          <TabsContent value="actors" className="pt-4">
            <ActorSubscriptions />
          </TabsContent>
        </Tabs>
      </div>
    </AppPage>
  )
}
