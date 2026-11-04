import { Link, useRouterState } from '@tanstack/react-router'
import type { LucideIcon } from 'lucide-react'

import { buttonVariants } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from 'cn'
import type { FileRoutesByTo } from '@/routeTree.gen'

type FloatingNavTo = keyof FileRoutesByTo

export type FloatingNavItem = {
  id: string
  icon: LucideIcon
  label: string
  to: FloatingNavTo
}

type FloatingNavProps = {
  items: FloatingNavItem[]
}

export function FloatingNav({ items }: FloatingNavProps) {
  return (
    <nav
      className="fixed bottom-6 left-1/2 z-50 flex -translate-x-1/2 items-center gap-0.5 rounded-full border border-border/80 bg-background/90 p-1.5 text-foreground shadow-lg shadow-black/5 backdrop-blur-md sm:bottom-8 sm:gap-1 sm:p-1.5"
      aria-label="主导航"
    >
      <ul className="flex items-center gap-0.5 sm:gap-1">
        {items.map(item => (
          <NavItem key={item.id} item={item} />
        ))}
      </ul>
    </nav>
  )
}

type NavItemProps = {
  item: FloatingNavItem
}

function NavItem({ item }: NavItemProps) {
  const pathname = useRouterState({ select: s => s.location.pathname })
  const isActive =
    item.to === '/'
      ? pathname === '/'
      : pathname === item.to || pathname.startsWith(`${item.to}/`)

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Link
          to={item.to}
          activeOptions={{ exact: item.to === '/', includeSearch: false }}
          className={cn(
            isActive
              ? cn(
                  buttonVariants({ variant: 'default', size: 'sm' }),
                  'h-11 gap-1.5 rounded-full px-3 sm:h-9 sm:px-2.5'
                )
              : cn(
                  buttonVariants({ variant: 'ghost', size: 'icon' }),
                  'size-11 rounded-full sm:size-9'
                )
          )}
          aria-current={isActive ? 'page' : undefined}
        >
          <item.icon className="size-5 shrink-0" />
          {isActive ? (
            <span className="max-w-16 truncate text-xs font-medium sm:max-w-20">{item.label}</span>
          ) : (
            <span className="sr-only">{item.label}</span>
          )}
        </Link>
      </TooltipTrigger>
      <TooltipContent side="top">{item.label}</TooltipContent>
    </Tooltip>
  )
}
