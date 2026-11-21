import { PlayIcon } from 'lucide-react'

import type { LibraryMovie } from '@/api/library'
import { MovieCard } from '@/components/movie'
import { PlayOverlay } from '@/components/movie/play-overlay'
import { MovieDetailTrigger } from '@/features/movie-detail/detail-trigger'
import { LibraryMovieStatus } from './movie-status'
import { LibraryMovieActions } from './movie-actions'

/** The fields the library card actually renders; kept narrow so callers pass
 * only what the card needs (e.g. a history item) without fabricating a movie. */
export type LibraryMovieCardData = {
  id: number
  code: string
  title: string
  cover?: string
  rating: number
  actors: Array<{ name: string }>
  source?: string
  has_subtitle?: boolean
  finished?: boolean
}

export function LibraryMovieCard({
  movie,
  onPlay,
  progress
}: {
  movie: LibraryMovieCardData
  onPlay?: () => void
  progress?: number
}) {
  const title = movie.title || movie.code
  const actors = movie.actors
    .map(actor => actor.name)
    .filter(Boolean)
    .join(' / ')

  return (
    <div className="group relative h-full min-w-0">
      <MovieDetailTrigger
        movie={{ libraryId: movie.id }}
        className="block h-full min-w-0 rounded-2xl outline-ring"
      >
        <MovieCard movie={movie} titleTooltip={false}>
          <div className="flex min-h-7 items-center pr-9">
            <LibraryMovieStatus movie={movie} />
          </div>
        </MovieCard>
      </MovieDetailTrigger>
      {/* Sibling of the detail trigger, not a child: an anchor inside a
          role="button" wrapper is invalid and swallows the detail click.
          `hideOnTouch` keeps touch behaviour (tap opens details). */}
      <PlayOverlay movieId={movie.id} label={movie.code} hideOnTouch />
      <div className="absolute right-2 bottom-2">
        <LibraryMovieActions movie={movie} disabled={busy} />
      </div>
    </div>
  )
}
