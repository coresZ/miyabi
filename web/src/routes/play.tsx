import { createFileRoute } from '@tanstack/react-router'

import { PlayerPage } from '@/features/player/page'

export const Route = createFileRoute('/play')({
  validateSearch: (search: Record<string, unknown>) => {
    const movie = Number(search.movie)
    return {
      movie: Number.isInteger(movie) && movie > 0 ? movie : undefined
    }
  },
  component: PlayerPage
})
