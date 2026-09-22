import { AppPage } from '@/components/app-page'
import { PageHeader } from '@/components/page-header'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { type DiscoverView, useDiscoverStore } from '@/stores/discover'
import { BrowseResults } from './browse-results'
import { CategoryContent } from './category-content'
import { DISCOVER_PAGE_SIZE as PAGE_SIZE } from './constants'

export function DiscoverPage() {
  const view = useDiscoverStore(state => state.view)
  const pages = useDiscoverStore(state => state.pages)
  const setView = useDiscoverStore(state => state.setView)
  const setPage = useDiscoverStore(state => state.setPage)
  const page = pages[view]

  return (
    <AppPage>
      <PageHeader title="发现" description="浏览 JavDB 的最新发行、即将发行和分类内容" />
      <div className="min-w-0 space-y-6">
        <Tabs value={view} onValueChange={value => setView(value as DiscoverView)}>
          <TabsList>
            <TabsTrigger value="released">最新</TabsTrigger>
            <TabsTrigger value="upcoming">即将发行</TabsTrigger>
            <TabsTrigger value="category">分类浏览</TabsTrigger>
          </TabsList>
        </Tabs>

        {view === 'category' ? (
          <CategoryContent page={page} onPageChange={next => setPage('category', next)} />
        ) : (
          <BrowseResults
            key={view}
            params={{
              page,
              limit: PAGE_SIZE,
              order: 'desc',
              ...(view === 'released' ? { main: ['m'], sort: 'update' } : { sort: 'release' })
            }}
            onPageChange={next => setPage(view, next)}
          />
        )}
      </div>
    </AppPage>
  )
}
