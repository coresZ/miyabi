import { StarIcon } from 'lucide-react'

import type { DiscoverMovie } from '@/api/discover'
import { MovieCard } from '@/components/movie'
import { MovieStateBadge } from '@/components/movie/movie-badges'
import { MovieDetailTrigger } from '@/features/movie-detail/detail-trigger'
import { MovieSubscribeButton } from './movie-subscribe-button'

export function DiscoverMovieCard({
  movie,
  allowSubscribe = true
}: {
  movie: DiscoverMovie
  allowSubscribe?: boolean
}) {
  const subscribable = allowSubscribe && movie.release_status === 'upcoming' && movie.magnets_count === 0
  const title = movie.title || movie.code
  return (
    <MovieDetailTrigger
      movie={{ id: movie.id, code: movie.code }}
      className="block h-full min-w-0 rounded-2xl outline-ring"
    >
      <MovieCard
        movie={movie}
        state={<MovieStateBadge movie={movie} />}
        coverOverlay={
          <>
            {subscribable ? (
              <div className="absolute top-2 right-2">
                <MovieSubscribeButton movie={movie} />
              </div>
            ) : null}
            {movie.release_date ? (
              <span className="absolute right-2 bottom-2 text-[0.65rem] font-medium text-white tabular-nums drop-shadow-[0_1px_2px_rgba(0,0,0,0.7)]">
                {movie.release_date}
              </span>
            ) : null}
          </>
        }
      />
    </MovieDetailTrigger>
  )
}
