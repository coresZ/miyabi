import { Link } from '@tanstack/react-router'
import { PlayIcon } from 'lucide-react'

import { cn } from 'cn'

/**
 * The transparent play affordance used by library cards and the detail cover.
 * `hideOnTouch` preserves the list behaviour (tap opens details); the detail
 * cover has no competing tap target, so it keeps the button visible on touch.
 */
export function PlayOverlay({
  movieId,
  label,
  hideOnTouch = false
}: {
  movieId: number
  label: string
  hideOnTouch?: boolean
}) {
  return (
    <Link
      to="/play"
      search={{ movie: movieId }}
      aria-label={`播放 ${label}`}
      className={cn(
        'absolute inset-x-0 top-0 z-10 grid aspect-3/2 place-items-center focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
        hideOnTouch && 'pointer-coarse:hidden'
      )}
    >
      <span
        className={cn(
          'grid size-14 place-items-center rounded-full border border-white/30 bg-black/40 text-white opacity-0 shadow-lg backdrop-blur-sm transition duration-150 group-focus-within:opacity-100 group-hover:opacity-100',
          !hideOnTouch && 'pointer-coarse:opacity-100'
        )}
      >
        <PlayIcon className="size-6 translate-x-0.5" />
      </span>
    </Link>
  )
}
