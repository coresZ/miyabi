import type { LibraryMovie } from '@/api/library'
import { Badge } from '@/components/ui/badge'

const STATUS_BADGE = 'h-4 px-1.5 text-[0.65rem] sm:h-5 sm:px-2 sm:text-xs'

export function LibraryMovieStatus({ movie }: { movie: LibraryMovie }) {
  return (
    <>
      {movie.scrape_status === 'done' ? (
        <Badge variant="success" className={STATUS_BADGE}>
          已刮削
        </Badge>
      ) : movie.scrape_status === 'failed' ? (
        <Badge
          variant="destructive"
          className={`${STATUS_BADGE} [--destructive:oklch(0.577_0.245_27.325)]`}
        >
          刮削失败
        </Badge>
      ) : (
        <Badge variant="outline" className={STATUS_BADGE}>
          待刮削
        </Badge>
      )}
    </>
  )
}
