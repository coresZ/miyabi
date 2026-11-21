import { BellIcon, CompassIcon, FilmIcon, PlayIcon, SearchIcon, SettingsIcon } from 'lucide-react'
import type { PropsWithChildren } from 'react'

import { AppSidebar } from '@/components/app-sidebar'
import { SearchDialog, SearchTrigger } from '@/components/app-search'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { Toaster } from '@/components/ui/sonner'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { TaskEventsProvider } from '@/features/tasks/task-events'
import { TaskNotifications } from '@/features/tasks/task-notifications'
import { useShellChrome } from '@/stores/shell-chrome'

const NAV_ITEMS: FloatingNavItem[] = [
  { id: 'library', label: '媒体库', icon: FilmIcon, to: '/' },
  { id: 'play', label: '播放', icon: PlayIcon, to: '/play' },
  { id: 'subscriptions', label: '订阅', icon: BellIcon, to: '/subscriptions' },
  { id: 'discover', label: '发现', icon: CompassIcon, to: '/discover' },
  { id: 'search', label: '搜索', icon: SearchIcon, to: '/search' },
  { id: 'settings', label: '设置', icon: SettingsIcon, to: '/settings' }
]

export function AppShell({ children }: PropsWithChildren) {
  const navHidden = useShellChrome(state => state.navHidden)
  return (
    <TaskEventsProvider>
      <div className="relative min-h-dvh">
        {navHidden ? null : <FloatingNav items={NAV_ITEMS} />}
        {children}
        <Toaster position="top-right" closeButton duration={6000} />
        <TaskNotifications />
      </div>
    </TaskEventsProvider>
  )
}

function AppHeader({ onOpenSearch }: { onOpenSearch: () => void }) {
  return (
    <header className="sticky top-0 z-50 flex h-12 shrink-0 items-center gap-2 overflow-hidden rounded-t-[inherit] border-b bg-background/50 px-4 backdrop-blur-md lg:px-6">
      <div className="flex shrink-0 items-center gap-1 lg:gap-2">
        <SidebarTrigger className="-ml-1" />
        <Separator
          orientation="vertical"
          className="mx-1 data-[orientation=vertical]:h-4 data-[orientation=vertical]:self-center lg:mx-2"
        />
        <SearchTrigger onOpen={onOpenSearch} />
      </div>

      <div className="flex min-w-0 flex-1 items-center" />

      <div className="flex shrink-0 items-center gap-3">
        <ThemeSwitcher />
      </div>
    </header>
  )
}

function ThemeSwitcher() {
  const { theme, setTheme } = useTheme()
  const nextTheme = THEME_CYCLE[(THEME_CYCLE.indexOf(theme) + 1) % THEME_CYCLE.length] ?? 'system'

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="icon-sm"
            aria-label="切换主题"
            onClick={() => setTheme(nextTheme)}
          />
        }
      >
        {theme === 'system' ? <MonitorIcon /> : theme === 'dark' ? <SunIcon /> : <MoonIcon />}
      </TooltipTrigger>
      <TooltipContent side="bottom" sideOffset={8}>
        切换主题
      </TooltipContent>
    </Tooltip>
  )
}
