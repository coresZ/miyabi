import {
  ArrowLeftIcon,
  ChevronRightIcon,
  EyeIcon,
  EyeOffIcon,
  Maximize2Icon,
  Minimize2Icon,
  SearchIcon,
  SquareIcon,
  XIcon
} from 'lucide-react'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent
} from 'react'

import { useLocalSources } from '@/api/local-sources'
import { usePanAccount } from '@/api/pan'
import { Link } from '@tanstack/react-router'
import { playableVideoUrl, usePlayables, type Playable, type PlayableKind } from '@/api/playables'
import { EmptyState } from '@/components/empty-state'
import { ErrorState, InlineError } from '@/components/error-state'
import { Button, buttonVariants } from '@/components/ui/button'
import { cn } from 'cn'
import { useShellChrome } from '@/stores/shell-chrome'
import { PlayerStage, type PlayerInstance, type PlayerTrack } from './player-stage'
import { isDesktopWindow, setPlayerWindowMode, type PlayerWindowMode } from './window-mode'

type SourceOption = {
  id: string
  label: string
  scope: PlayableKind
  directory: string
  disabled?: boolean
}

const ALL_LOCAL: SourceOption = {
  id: 'local:all',
  label: '本地（全部）',
  scope: 'local',
  directory: ''
}

type SheetSnap = 'peek' | 'half' | 'full'
const SHEET_SNAPS: SheetSnap[] = ['peek', 'half', 'full']
const SHEET_SNAP_VH: Record<SheetSnap, number> = { peek: 30, half: 56, full: 88 }

const RAIL_PREF_KEY = 'miyabi.play.rail'
const DEFAULT_RAIL = 320
const MIN_RAIL = 200
const MAX_RAIL = 520
const RAIL_COLLAPSE_AT = 150

function readRailPref(key: 'width'): number
function readRailPref(key: 'collapsed'): boolean
function readRailPref(key: 'width' | 'collapsed'): number | boolean {
  try {
    const raw = window.localStorage.getItem(RAIL_PREF_KEY)
    const parsed = raw ? (JSON.parse(raw) as { width?: number; collapsed?: boolean }) : {}
    if (key === 'width') {
      const width = Number(parsed.width)
      return Number.isFinite(width) ? Math.min(MAX_RAIL, Math.max(MIN_RAIL, width)) : DEFAULT_RAIL
    }
    return !!parsed.collapsed
  } catch {
    return key === 'width' ? DEFAULT_RAIL : false
  }
}

function useIsDesktop() {
  const [isDesktop, setIsDesktop] = useState(false)
  useEffect(() => {
    const query = window.matchMedia('(min-width: 1024px)')
    const update = () => setIsDesktop(query.matches)
    update()
    query.addEventListener('change', update)
    return () => query.removeEventListener('change', update)
  }, [])
  return isDesktop
}

export function PlayerPage() {
  const sources = useLocalSources()
  const pan = usePanAccount()
  const [sourceID, setSourceID] = useState(ALL_LOCAL.id)
  const [search, setSearch] = useState('')
  const [term, setTerm] = useState('')
  const [current, setCurrent] = useState(-1)
  const [playing, setPlaying] = useState<PlayerTrack | null>(null)
  const [collapsed, setCollapsed] = useState(() => readRailPref('collapsed'))
  const [railWidth, setRailWidth] = useState(() => readRailPref('width'))
  const [draggingRail, setDraggingRail] = useState(false)
  const [controlsHidden, setControlsHidden] = useState(false)
  const [snap, setSnap] = useState<SheetSnap>('half')
  const [dragging, setDragging] = useState(false)
  const [dragHeight, setDragHeight] = useState<number | null>(null)
  const playerRef = useRef<PlayerInstance | null>(null)
  const playerSectionRef = useRef<HTMLElement | null>(null)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const searchTimer = useRef<number | undefined>(undefined)
  const dragRef = useRef<{ startY: number; startHeight: number; height: number } | null>(null)
  const railDragRef = useRef<{ startX: number; startWidth: number } | null>(null)
  const railMovedRef = useRef(false)
  const isDesktop = useIsDesktop()
  const desktopShell = isDesktopWindow()
  const setNavHidden = useShellChrome(state => state.setNavHidden)
  const [windowMode, setWindowMode] = useState<PlayerWindowMode>('windowed')
  const [chromeVisible, setChromeVisible] = useState(false)
  const [railOpen, setRailOpen] = useState(false)
  const [sourceAspect, setSourceAspect] = useState<number | null>(null)
  const [coverVideo, setCoverVideo] = useState(false)
  const borderless = windowMode !== 'windowed'

  // A source is cropped only when the window no longer matches its ratio, so a
  // freshly entered borderless window keeps the full frame.
  useEffect(() => {
    const update = () => {
      if (!sourceAspect || !borderless) {
        setCoverVideo(false)
        return
      }
      const frame = window.innerWidth / window.innerHeight
      setCoverVideo(Math.abs(frame - sourceAspect) > 0.01)
    }
    update()
    window.addEventListener('resize', update)
    return () => window.removeEventListener('resize', update)
  }, [sourceAspect, borderless])

  const applyWindowMode = useCallback(
    (mode: PlayerWindowMode) => {
      setWindowMode(mode)
      if (mode !== 'borderless') setRailOpen(false)
      void setPlayerWindowMode(mode, mode === 'windowed' ? null : sourceAspect).catch(() =>
        setWindowMode('windowed')
      )
    },
    [sourceAspect]
  )

  // The play page hides the floating navigation in the desktop shell: on this
  // page the nav sits on top of the player controls, and the toolbar already
  // carries a way back to the library.
  useEffect(() => {
    if (!desktopShell) return
    setNavHidden(true)
    return () => setNavHidden(false)
  }, [desktopShell, setNavHidden])

  // Escape steps back one level: fullscreen -> borderless -> windowed.
  useEffect(() => {
    if (windowMode === 'windowed') return
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      applyWindowMode(windowMode === 'fullscreen' ? 'borderless' : 'windowed')
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [windowMode, applyWindowMode])

  // Leaving the page must restore the title bar, otherwise the window stays
  // borderless with no way back.
  useEffect(() => {
    if (!desktopShell) return
    return () => {
      void setPlayerWindowMode('windowed')
    }
  }, [desktopShell])
  const [viewport, setViewport] = useState(() => ({
    height: typeof window === 'undefined' ? 0 : window.innerHeight,
    playerBottom: 0
  }))

  const options = useMemo<SourceOption[]>(() => {
    const list: SourceOption[] = [ALL_LOCAL]
    for (const source of sources.data ?? []) {
      if (!source.enabled) continue
      list.push({
        id: `local:${source.id}`,
        label: source.name,
        scope: 'local',
        directory: source.id
      })
    }
    if (pan.data?.connected && pan.data.directory) {
      list.push({
        id: 'remote',
        label: `115 · ${pan.data.directory.name}`,
        scope: 'remote',
        directory: ''
      })
    }
    list.push({ id: 'favorite', label: '收藏', scope: 'favorite', directory: '', disabled: true })
    return list
  }, [sources.data, pan.data])

  const active = options.find(option => option.id === sourceID) ?? ALL_LOCAL
  const query = usePlayables(active.scope, active.directory, term)
  const {
    isPending,
    isError,
    isFetching,
    isRefetchError,
    isFetchingNextPage,
    hasNextPage,
    fetchNextPage
  } = query
  const reload = () => void query.refetch()

  const items = useMemo(() => query.data?.pages.flatMap(page => page.items) ?? [], [query.data])
  const tracks = useMemo<PlayerTrack[]>(
    () =>
      items.flatMap(item =>
        item.videos.map(video => ({
          url: playableVideoUrl(item.kind, video.file_id),
          name: item.title || item.code,
          code: item.code
        }))
      ),
    [items]
  )
  const movies = useMemo(() => {
    const result: { item: Playable; start: number; count: number }[] = []
    items.reduce((offset, item) => {
      result.push({ item, start: offset, count: item.videos.length })
      return offset + item.videos.length
    }, 0)
    return result
  }, [items])
  const activeMovie = movies.find(
    entry => current >= entry.start && current < entry.start + entry.count
  )

  useEffect(() => {
    playerRef.current?.setQueue(tracks)
  }, [tracks])

  useEffect(() => {
    playerRef.current?.setControlsHidden(controlsHidden)
  }, [controlsHidden])

  useEffect(() => {
    const element = sentinelRef.current
    if (!element || !hasNextPage) return
    const observer = new IntersectionObserver(entries => {
      if (entries[0]?.isIntersecting) void fetchNextPage()
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [hasNextPage, fetchNextPage, items.length])

  useEffect(() => () => window.clearTimeout(searchTimer.current), [])

  useEffect(() => {
    const update = () =>
      setViewport({
        height: window.innerHeight,
        playerBottom: playerSectionRef.current?.getBoundingClientRect().bottom ?? 0
      })
    update()
    const observer = new ResizeObserver(update)
    if (playerSectionRef.current) observer.observe(playerSectionRef.current)
    window.addEventListener('resize', update)
    return () => {
      observer.disconnect()
      window.removeEventListener('resize', update)
    }
  }, [])

  useEffect(() => {
    window.localStorage.setItem(RAIL_PREF_KEY, JSON.stringify({ width: railWidth, collapsed }))
  }, [railWidth, collapsed])

  const onSearch = (value: string) => {
    setSearch(value)
    window.clearTimeout(searchTimer.current)
    searchTimer.current = window.setTimeout(() => {
      setTerm(value.trim())
      setCurrent(-1)
    }, 250)
  }

  const play = (index: number) => {
    const track = tracks[index]
    if (!track) return
    playerRef.current?.setSources(tracks, index)
    setCurrent(index)
    setPlaying(track)
    if (borderless) setRailOpen(false)
    if (!isDesktop) setSnap('half')
  }

  const partIndex = activeMovie ? current - activeMovie.start + 1 : 0
  const go = (delta: number) => {
    const next = current + delta
    if (next >= 0 && next < tracks.length) play(next)
  }
  const countLabel = `${tracks.length}${hasNextPage ? '+' : ''} 个文件`

  // peek / half 档限制在播放器下方（不遮挡视频底部操作条），full 档才允许覆盖。
  const snapHeight = (value: SheetSnap) => {
    const height = viewport.height || window.innerHeight
    const below = Math.max(150, height - viewport.playerBottom - 12)
    if (value === 'full') return Math.round(height * (SHEET_SNAP_VH.full / 100))
    return Math.round(Math.min(height * (SHEET_SNAP_VH[value] / 100), below))
  }
  const sheetHeight = snapHeight(snap)

  const onHandleDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    const height = snapHeight(snap)
    dragRef.current = { startY: event.clientY, startHeight: height, height }
    setDragging(true)
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  const onHandleMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag) return
    const next = Math.min(
      snapHeight('full'),
      Math.max(snapHeight('peek'), drag.startHeight + drag.startY - event.clientY)
    )
    drag.height = next
    setDragHeight(next)
  }

  const onHandleUp = () => {
    const drag = dragRef.current
    dragRef.current = null
    setDragging(false)
    setDragHeight(null)
    if (!drag) return
    if (Math.abs(drag.height - drag.startHeight) < 6) {
      setSnap(value => (value === 'peek' ? 'half' : value === 'half' ? 'full' : 'peek'))
      return
    }
    let nearest: SheetSnap = 'peek'
    for (const value of SHEET_SNAPS) {
      if (Math.abs(snapHeight(value) - drag.height) < Math.abs(snapHeight(nearest) - drag.height)) {
        nearest = value
      }
    }
    setSnap(nearest)
  }

  // 桌面右栏：把手拖拽调宽 / 点击收起展开 / 双击复位
  const onRailDown = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (!isDesktop) return
    railMovedRef.current = false
    railDragRef.current = { startX: event.clientX, startWidth: collapsed ? 0 : railWidth }
    setDraggingRail(true)
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  const onRailMove = (event: ReactPointerEvent<HTMLButtonElement>) => {
    const drag = railDragRef.current
    if (!drag) return
    const next = drag.startWidth + (drag.startX - event.clientX)
    if (Math.abs(next - drag.startWidth) > 3) railMovedRef.current = true
    if (next < RAIL_COLLAPSE_AT) {
      setCollapsed(true)
      return
    }
    setCollapsed(false)
    setRailWidth(Math.min(MAX_RAIL, Math.max(MIN_RAIL, next)))
  }

  const onRailUp = () => {
    railDragRef.current = null
    setDraggingRail(false)
  }

  const onRailClick = () => {
    if (railMovedRef.current) {
      railMovedRef.current = false
      return
    }
    setCollapsed(value => !value)
  }

  const resetRail = () => {
    setRailWidth(DEFAULT_RAIL)
    setCollapsed(false)
  }

  return (
    <main
      className={cn(
        'mx-auto flex h-dvh w-full max-w-[84rem] flex-col gap-5 overflow-hidden bg-background px-4 pt-7',
        // The floating nav is hidden in the desktop shell, so its reserved lane
        // would just be an empty strip under the player.
        desktopShell ? 'pb-4' : 'pb-24',
        // Borderless only shows the video: pin the page to the window edges so no
        // page padding or max-width leaves a gap around the player.
        borderless && 'fixed inset-0 z-40 h-auto w-auto max-w-none gap-0 bg-black p-0'
      )}
    >
      {borderless ? (
        <>
          {/* Invisible hover strip: the window controls appear only when the
              pointer reaches the top edge, so the video keeps the whole frame. */}
          <div
            aria-hidden
            className="absolute inset-x-0 top-0 z-40 h-12"
            onPointerEnter={() => setChromeVisible(true)}
            onPointerLeave={() => setChromeVisible(false)}
          />
          {/* Right-edge strip: the catalog slides in without covering the video. */}
          <div
            aria-hidden
            className="absolute inset-y-0 right-0 z-40 w-6"
            onPointerEnter={() => setRailOpen(true)}
          />
          <div
            data-tauri-drag-region
            className={cn(
              'absolute inset-x-0 top-0 z-50 flex h-9 items-center justify-end bg-gradient-to-b from-black/70 to-transparent pr-1 transition-opacity duration-200 select-none',
              chromeVisible ? 'opacity-100' : 'pointer-events-none opacity-0'
            )}
            onPointerEnter={() => setChromeVisible(true)}
            onPointerLeave={() => setChromeVisible(false)}
          >
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="还原窗口"
                title="还原窗口"
                className="text-white hover:bg-white/20 hover:text-white"
                onClick={() => applyWindowMode('windowed')}
              >
                <Minimize2Icon />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={windowMode === 'fullscreen' ? '退出全屏' : '全屏'}
                title={windowMode === 'fullscreen' ? '退出全屏' : '全屏'}
                className="text-white hover:bg-white/20 hover:text-white"
                onClick={() =>
                  applyWindowMode(windowMode === 'fullscreen' ? 'borderless' : 'fullscreen')
                }
              >
                {windowMode === 'fullscreen' ? <SquareIcon /> : <Maximize2Icon />}
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="关闭窗口"
                title="关闭窗口"
                className="text-white hover:bg-red-500 hover:text-white"
                onClick={() => {
                  const w = window as Window & {
                    __TAURI__?: {
                      window?: { getCurrentWindow?: () => { close: () => Promise<void> } }
                    }
                  }
                  void w.__TAURI__?.window?.getCurrentWindow?.().close()
                }}
              >
                <XIcon />
              </Button>
            </div>
          </div>
        </>
      ) : null}

      {desktopShell ? null : (
        <header className="flex flex-none flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="text-3xl font-bold tracking-tight">播放</h1>
            <p className="mt-1 text-sm text-muted-foreground">选择一个来源，点击右侧目录即可播放</p>
          </div>
          <span className="hidden text-xs text-muted-foreground tabular-nums sm:inline">
            {active.label} · {countLabel}
          </span>
        </header>
      )}

      <div
        style={
          isDesktop && !borderless
            ? ({ '--rail-w': collapsed ? '0px' : `${railWidth}px` } as CSSProperties)
            : undefined
        }
        className={cn(
          'relative grid min-h-0 flex-1 grid-rows-[auto] gap-5 lg:grid-cols-[minmax(0,1fr)_var(--rail-w)] lg:grid-rows-1',
          borderless && 'h-full gap-0 lg:grid-cols-[minmax(0,1fr)]',
          draggingRail
            ? 'lg:transition-none'
            : 'lg:transition-[grid-template-columns] lg:duration-200 lg:ease-out'
        )}
      >
        <section
          ref={playerSectionRef}
          className={cn(
            'flex min-h-0 min-w-0 flex-col gap-3 self-start lg:self-stretch',
            borderless && 'gap-0 self-stretch'
          )}
        >
          <PlayerStage
            cover={coverVideo}
            className={cn(borderless && 'aspect-auto h-full w-full flex-1 rounded-none')}
            onMetadata={(width, height) => {
              const aspect = width / height
              setSourceAspect(aspect)
              if (borderless) void setPlayerWindowMode('borderless', aspect)
            }}
            onReady={player => {
              playerRef.current = player
              player.setQueue(tracks)
              player.setControlsHidden(controlsHidden)
            }}
            onChange={(index, source) => {
              setCurrent(index)
              setPlaying(source)
            }}
            onEnded={(_index, hasNext) => {
              if (!hasNext && hasNextPage) void fetchNextPage()
            }}
          />

          <div className={cn('mt-auto flex min-w-0 items-center gap-3', borderless && 'hidden')}>
            {playing ? (
              <>
                <span className="flex-none rounded-full border border-border px-2.5 py-1 text-xs font-semibold tabular-nums">
                  {playing.code}
                </span>
                <span className="min-w-0 flex-1 truncate text-sm font-medium">{playing.name}</span>
                {activeMovie && activeMovie.count > 1 ? (
                  <span className="flex-none text-xs text-muted-foreground tabular-nums">
                    第 {partIndex} / {activeMovie.count} 段
                  </span>
                ) : null}
              </>
            ) : (
              <span className="text-sm text-muted-foreground">未播放</span>
            )}
            <div className="ml-auto flex flex-none gap-2">
              {desktopShell ? (
                <Link
                  to="/"
                  aria-label="返回媒体库"
                  title="返回媒体库"
                  className={cn(buttonVariants({ variant: 'outline', size: 'icon-sm' }))}
                >
                  <ArrowLeftIcon />
                </Link>
              ) : null}
              <Button
                variant="outline"
                size="icon-sm"
                aria-pressed={controlsHidden}
                aria-label={controlsHidden ? '显示控制面板' : '收起控制面板'}
                title={controlsHidden ? '显示控制面板' : '收起控制面板'}
                onClick={() => setControlsHidden(value => !value)}
              >
                {controlsHidden ? <EyeOffIcon /> : <EyeIcon />}
              </Button>
              {desktopShell ? (
                <Button
                  variant="outline"
                  size="icon-sm"
                  aria-label="无边框播放"
                  title="无边框播放"
                  onClick={() => applyWindowMode('borderless')}
                >
                  <Maximize2Icon />
                </Button>
              ) : null}
              <Button variant="outline" size="sm" disabled={current <= 0} onClick={() => go(-1)}>
                上一段
              </Button>
              <Button
                size="sm"
                disabled={current < 0 || current + 1 >= tracks.length}
                onClick={() => go(1)}
              >
                下一段
              </Button>
            </div>
          </div>
        </section>

        <section
          inert={collapsed && isDesktop && !borderless ? true : undefined}
          style={isDesktop ? undefined : { height: dragHeight ?? sheetHeight }}
          className={cn(
            'flex min-h-0 flex-col overflow-hidden rounded-2xl bg-card ring-1 ring-border/60 lg:transition-opacity lg:duration-200',
            'max-lg:fixed max-lg:inset-x-0 max-lg:bottom-0 max-lg:z-40 max-lg:rounded-b-none max-lg:shadow-[0_-10px_34px_rgba(0,0,0,0.14)]',
            collapsed && isDesktop && !borderless && 'lg:pointer-events-none lg:opacity-0',
            // Borderless keeps the catalog off-screen and slides it in from the
            // right edge, so the video owns the whole frame.
            borderless &&
              cn(
                'fixed inset-y-0 right-0 z-50 w-[20rem] max-w-[80vw] rounded-none rounded-l-2xl bg-card/95 shadow-[-10px_0_34px_rgba(0,0,0,0.35)] backdrop-blur transition-transform duration-200 ease-out',
                railOpen ? 'translate-x-0' : 'translate-x-full'
              ),
            dragging
              ? 'max-lg:transition-none'
              : 'max-lg:transition-[height] max-lg:duration-200 max-lg:ease-out'
          )}
          onPointerLeave={() => {
            if (borderless) setRailOpen(false)
          }}
        >
          <div
            onPointerDown={onHandleDown}
            onPointerMove={onHandleMove}
            onPointerUp={onHandleUp}
            onPointerCancel={onHandleUp}
            className="flex flex-none cursor-grab touch-none items-center justify-center py-2.5 active:cursor-grabbing lg:hidden"
          >
            <span className="h-1 w-9 rounded-full bg-border" />
          </div>
          <div className="flex-none border-b border-border/60 p-3">
            <div className="flex items-center gap-2">
              <h2 className="text-sm font-bold">目录列表</h2>
              <span className="text-xs text-muted-foreground tabular-nums">{countLabel}</span>
            </div>
            <div className="relative mt-2">
              <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <input
                value={search}
                onChange={event => onSearch(event.target.value)}
                placeholder="搜索番号或标题"
                className="h-9 w-full rounded-lg border border-border bg-background pr-3 pl-8 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            </div>
            <div className="mt-2 flex [scrollbar-width:none] gap-1.5 overflow-x-auto pb-0.5 [&::-webkit-scrollbar]:hidden">
              {options.map(option => (
                <button
                  key={option.id}
                  type="button"
                  disabled={option.disabled}
                  onClick={() => {
                    setCurrent(-1)
                    setSourceID(option.id)
                  }}
                  className={cn(
                    'h-7 flex-none rounded-full border px-2.5 text-xs whitespace-nowrap transition-colors',
                    option.id === sourceID
                      ? 'border-transparent bg-primary text-primary-foreground'
                      : 'border-border text-muted-foreground hover:bg-muted hover:text-foreground',
                    option.disabled
                      ? 'cursor-not-allowed opacity-45 hover:bg-transparent'
                      : 'cursor-pointer'
                  )}
                >
                  {option.label}
                </button>
              ))}
            </div>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-1.5 pt-1.5 pb-24 lg:pb-1.5">
            {isPending ? (
              <p className="py-8 text-center text-sm text-muted-foreground">加载中…</p>
            ) : isError ? (
              <div className="p-2">
                <ErrorState message="无法读取播放列表" onRetry={reload} retrying={isFetching} />
              </div>
            ) : movies.length === 0 ? (
              <EmptyState
                className="py-10"
                title={term ? '没有匹配的影片' : '这个来源还没有可播放的影片'}
              />
            ) : (
              <>
                {isRefetchError ? (
                  <InlineError onRetry={reload} retrying={isFetching}>
                    刷新失败，请重试。
                  </InlineError>
                ) : null}
                {movies.map(entry => {
                  const isCurrent = activeMovie?.item.movie_id === entry.item.movie_id
                  return (
                    <button
                      key={entry.item.movie_id}
                      type="button"
                      onClick={() => play(entry.start)}
                      aria-current={isCurrent ? 'true' : undefined}
                      className={cn(
                        'flex w-full cursor-pointer items-center gap-3 rounded-xl px-2 py-2 text-left transition-colors hover:bg-muted',
                        isCurrent && 'bg-accent ring-1 ring-border/60'
                      )}
                    >
                      <span className="grid aspect-video w-16 flex-none place-items-center overflow-hidden rounded-lg bg-muted text-[0.6rem] text-muted-foreground ring-1 ring-border/60">
                        {entry.item.poster ? (
                          <img
                            src={entry.item.poster}
                            alt=""
                            className="h-full w-full object-cover"
                          />
                        ) : (
                          '封面'
                        )}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="inline-block rounded-full border border-border px-1.5 py-0.5 text-[0.6rem] font-semibold tabular-nums">
                          {entry.item.code}
                        </span>
                        <span className="mt-1 block truncate text-sm">
                          {entry.item.title || entry.item.code}
                        </span>
                      </span>
                    </button>
                  )
                })}
                <div ref={sentinelRef} className="h-6" />
                {isFetchingNextPage ? (
                  <p className="py-3 text-center text-sm text-muted-foreground">加载中…</p>
                ) : null}
              </>
            )}
          </div>
        </section>

        {isDesktop && !borderless ? (
          <button
            type="button"
            aria-label={collapsed ? '展开目录' : '收起目录'}
            aria-expanded={!collapsed}
            title="点击收起 / 展开，拖拽调宽，双击复位"
            onPointerDown={onRailDown}
            onPointerMove={onRailMove}
            onPointerUp={onRailUp}
            onPointerCancel={onRailUp}
            onClick={onRailClick}
            onDoubleClick={resetRail}
            style={{
              left: 'min(calc(100% - var(--rail-w) - 10px), calc(100% - 12px))',
              transition: draggingRail ? 'none' : 'left 200ms ease-out'
            }}
            className="absolute top-1/2 z-10 grid h-[54px] w-[22px] -translate-x-1/2 -translate-y-1/2 cursor-col-resize touch-none place-items-center rounded-full border border-border bg-background text-foreground shadow-sm"
          >
            <ChevronRightIcon
              className={cn('size-[13px] transition-transform', collapsed && 'rotate-180')}
            />
          </button>
        ) : null}
      </div>
    </main>
  )
}
