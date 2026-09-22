import { useDiscoverTags } from '@/api/discover'
import { useDiscoverStore } from '@/stores/discover'
import { BrowseResults } from './browse-results'
import { CategoryFilters, MAIN_CATEGORY, UNSUPPORTED_CATEGORIES } from './category-filters'
import { categoryBrowseParams } from './category-params'
import { DISCOVER_PAGE_SIZE as PAGE_SIZE } from './constants'

export function CategoryContent({
  page,
  onPageChange
}: {
  page: number
  onPageChange: (page: number) => void
}) {
  const category = useDiscoverStore(state => state.category)
  const { zone, categoryID } = category
  const taxonomy = useDiscoverTags(zone)
  const categories = (taxonomy.data ?? []).filter(
    item => item.id !== MAIN_CATEGORY && !UNSUPPORTED_CATEGORIES.has(item.id)
  )
  const selectedCategory = categories.find(item => item.id === categoryID) ?? categories[0]

  return (
    <div className="space-y-6">
      <CategoryFilters />
      <BrowseResults
        params={{
          page,
          limit: PAGE_SIZE,
          sort: 'release',
          order: 'desc',
          ...categoryBrowseParams({ ...category, categoryID: selectedCategory?.id ?? '' })
        }}
        onPageChange={onPageChange}
      />
    </div>
  )
}
