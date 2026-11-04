import type { ReactNode } from 'react'

import { cn } from 'cn'

type EmptyStateProps = {
  emoji?: string
  title: string
  description?: string
  actions?: ReactNode
  className?: string
}

export function EmptyState({
  emoji = '(˙ᯅ˙)',
  title,
  description,
  actions,
  className
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        'flex min-h-80 w-full flex-col items-center justify-center gap-3 text-center',
        className
      )}
    >
      <p className="text-5xl font-bold tracking-tight text-foreground/80">{emoji}</p>
      <div className="space-y-1.5">
        <p className="text-base font-medium text-foreground">{title}</p>
        {description ? (
          <p className="mx-auto max-w-sm text-sm text-muted-foreground">{description}</p>
        ) : null}
      </div>
      {actions ? <div className="pointer-events-auto mt-2">{actions}</div> : null}
    </div>
  )
}
