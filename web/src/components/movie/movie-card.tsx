import type { ReactNode } from 'react'

import { MovieCover } from '@/components/movie/movie-cover'
import { OverflowTooltip } from '@/components/overflow-tooltip'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'

export function MovieCard({
  movie,
  description,
  coverLoading = 'lazy',
  onCoverReady,
  coverOverlay,
  titleTooltip = true,
  state,
  children
}: {
  movie: { code: string; title: string; cover?: string }
  description?: ReactNode
  coverLoading?: 'eager' | 'lazy'
  onCoverReady?: () => void
  coverOverlay?: ReactNode
  titleTooltip?: boolean
  state?: ReactNode
  children?: ReactNode
}) {
  const title = movie.title || movie.code
  const heading = <h3 className="truncate text-sm leading-5 font-semibold">{title}</h3>
  return (
    <Card
      size="sm"
      className="group h-full gap-0 overflow-hidden border-border/80 py-0 shadow-sm ring-1 ring-black/3 transition-shadow hover:shadow-md dark:ring-white/5"
    >
      <div className="relative flex aspect-3/2 items-center justify-center overflow-hidden bg-muted">
        <div className="absolute inset-0 transition-[filter] duration-150 group-focus-within:brightness-[0.82] group-hover:brightness-[0.82]">
          <MovieCover
            source={movie.cover ?? ''}
            label={movie.code}
            loading={coverLoading}
            onReady={onCoverReady}
          />
        </div>
        <div className="absolute top-2 left-2 flex max-w-[calc(100%-3.5rem)] flex-wrap gap-1.5">
          <Badge variant="frosted" className="max-w-full truncate">
            {movie.code}
          </Badge>
          {state}
        </div>
        {coverOverlay}
      </div>
      <CardContent className="min-w-0 space-y-2 p-3">
        {titleTooltip ? <OverflowTooltip content={title}>{heading}</OverflowTooltip> : heading}
        {description != null ? (
          <div className="text-xs text-muted-foreground">{description}</div>
        ) : null}
        {children ? <div className="flex flex-wrap gap-1.5">{children}</div> : null}
      </CardContent>
    </Card>
  )
}
