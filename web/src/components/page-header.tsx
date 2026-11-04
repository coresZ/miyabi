import type { ReactNode } from 'react'

import { cn } from 'cn'

type PageHeaderProps = {
  title: string
  description: string
  children?: ReactNode
  inlineActions?: boolean
}

export function PageHeader({
  title,
  description,
  children,
  inlineActions = false
}: PageHeaderProps) {
  return (
    <header
      className={cn(
        'flex flex-col items-start justify-between gap-3 sm:flex-row sm:items-center',
        inlineActions ? 'flex-row items-center' : null
      )}
    >
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">{title}</h1>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      {children ? (
        <div className="flex max-w-full flex-wrap items-center gap-2">{children}</div>
      ) : null}
    </header>
  )
}
