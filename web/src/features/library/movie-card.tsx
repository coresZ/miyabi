import { Link } from '@tanstack/react-router'
import { PlayIcon } from 'lucide-react'

import type { LibraryMovie } from '@/api/library'
import { MovieCard } from '@/components/movie'
import { MovieDetailTrigger } from '@/features/movie-detail/detail-trigger'
import { LibraryMovieStatus } from './movie-status'
import { LibraryMovieActions } from './movie-actions'

export function LibraryMovieCard({ movie, busy = false }: { movie: LibraryMovie; busy?: boolean }) {
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
          `pointer-coarse:hidden` keeps touch behaviour (tap opens details). */}
      <Link
        to="/play"
        search={{ movie: movie.id }}
        aria-label={`播放 ${movie.code}`}
        className="absolute inset-x-0 top-0 z-10 grid aspect-3/2 place-items-center focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none pointer-coarse:hidden"
      >
        <span className="grid size-14 place-items-center rounded-full border border-white/30 bg-black/40 text-white opacity-0 shadow-lg backdrop-blur-sm transition duration-150 group-focus-within:opacity-100 group-hover:opacity-100">
          <PlayIcon className="size-6 translate-x-0.5" />
        </span>
      </Link>
      <div className="absolute right-2 bottom-2">
        <LibraryMovieActions movie={movie} disabled={busy} />
      </div>
    </div>
  )
}
