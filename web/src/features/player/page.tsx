import {
  ArrowLeftIcon,
  ChevronRightIcon,
  EyeIcon,
  EyeOffIcon,
  LockIcon,
  Maximize2Icon,
  PlayIcon,
  SearchIcon,
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
import { Link, useSearch } from '@tanstack/react-router'
import { toast } from 'sonner'

import { useLocalSources } from '@/api/local-sources'
import { usePanAccount } from '@/api/pan'
import { playableVideoUrl, usePlayables, type Playable, type PlayableKind } from '@/api/playables'
import { EmptyState } from '@/components/empty-state'
import { ErrorState, InlineError } from '@/components/error-state'
import { MediaImage } from '@/components/media-image'
import { Button, buttonVariants } from '@/components/ui/button'
import { cn } from 'cn'
import { getUiState, updateUiState, useUiState } from '@/stores/ui-state'
import { useShellChrome } from '@/stores/shell-chrome'
import { PlayerStage, type PlayerInstance, type PlayerTrack } from './player-stage'
import { readPlayerPrefs, savePlayerPrefs } from './prefs'
import {
  enterPlayerImmersive,
  isDesktopWindow,
  restoreNormalWindow,
  startWindowResize
} from './player-window'

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

const DEFAULT_RAIL = 320
const MIN_RAIL = 200
const MAX_RAIL = 520
const RAIL_COLLAPSE_AT = 150

function readRailPref(key: 'width'): number
function readRailPref(key: 'collapsed'): boolean
function readRailPref(key: 'width' | 'collapsed'): number | boolean {
  const rail = getUiState().rail
  if (key === 'width') {
    return Math.min(MAX_RAIL, Math.max(MIN_RAIL, rail.width || DEFAULT_RAIL))
  }
  return rail.collapsed
}

function useIsDesktop() {
  const [isDesktop, setIsDesktop] = useState(
    () => typeof window !== 'undefined' && window.matchMedia('(min-width: 1024px)').matches
  )
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
  const routeSearch = useSearch({ from: '/play' })
  const targetMovie = routeSearch.movie
  const appliedTarget = useRef<number | null>(null)
  const lookupAttempts = useRef(0)
  const triedRemote = useRef(false)
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
  const nsfwMode = useUiState(state => state.nsfwMode)
  const [immersive, setImmersive] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [nsfwRevealed, setNsfwRevealed] = useState(false)
  const [chromeVisible, setChromeVisible] = useState(false)
  const [railOpen, setRailOpen] = useState(false)
  const [prefs, setPrefs] = useState(readPlayerPrefs)
  const [sourceAspect, setSourceAspect] = useState<number | null>(null)
  const tracksRef = useRef<PlayerTrack[]>([])
  const pendingPlayRef = useRef<number | null>(null)
  const nsfwRevealedRef = useRef(false)
  const fittedAspectRef = useRef<number | null>(null)
  const gated = nsfwMode && !nsfwRevealed

  const enterImmersive = useCallback(async () => {
    setRailOpen(false)
    setImmersive(true)
    try {
      await enterPlayerImmersive(sourceAspect)
      fittedAspectRef.current = sourceAspect
    } catch {
      await restoreNormalWindow().catch(() => {})
      setImmersive(false)
    }
  }, [sourceAspect])

  const exitImmersive = useCallback(async () => {
    setImmersive(false)
    setRailOpen(false)
    fittedAspectRef.current = null
    await restoreNormalWindow().catch(() => {})
  }, [])

  // Refit when a new source reports a different aspect while immersive.
  useEffect(() => {
    if (!desktopShell || !immersive || fullscreen || !sourceAspect) return
    if (fittedAspectRef.current === sourceAspect) return
    fittedAspectRef.current = sourceAspect
    void enterPlayerImmersive(sourceAspect).catch(() => {})
  }, [desktopShell, immersive, fullscreen, sourceAspect])

  const onFullscreenChange = useCallback(
    (active: boolean) => {
      setFullscreen(active)
      if (active) return
      // Exiting fullscreen always returns to the normal page/window, never to
      // the immersive layout that might have been active before fullscreen.
      setImmersive(false)
      setRailOpen(false)
      fittedAspectRef.current = null
      if (!desktopShell) return
      // Tauri restores the pre-fullscreen window itself; let it finish before
      // applying our snapshot so the two restores cannot race.
      window.setTimeout(() => {
        void restoreNormalWindow().catch(() => {})
      }, 50)
    },
    [desktopShell]
  )

  const revealNsfw = useCallback(() => {
    nsfwRevealedRef.current = true
    setNsfwRevealed(true)
    const index = pendingPlayRef.current
    if (index == null || !playerRef.current) return
    playerRef.current.setSources(tracksRef.current, index)
    pendingPlayRef.current = null
  }, [])

  // The play page hides the floating navigation in the desktop shell: on this
  // page the nav sits on top of the player controls, and the toolbar already
  // carries a way back to the library.
  useEffect(() => {
    if (!desktopShell) return
    setNavHidden(true)
    return () => setNavHidden(false)
  }, [desktopShell, setNavHidden])

  // Escape closes the catalog first, then exits immersive. Fullscreen is left
  // to WebView2/the browser; the page never intercepts it.
  useEffect(() => {
    if (!immersive && !railOpen) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || document.fullscreenElement) return
      event.preventDefault()
      if (railOpen) {
        setRailOpen(false)
        return
      }
      if (immersive) void exitImmersive()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [immersive, railOpen, exitImmersive])

  // Leaving or reloading the page must restore the normal window; mounting also
  // clears a stale immersive window left by a reload.
  useEffect(() => {
    if (!desktopShell) return
    void restoreNormalWindow().catch(() => {})
    return () => {
      void restoreNormalWindow().catch(() => {})
    }
  }, [desktopShell])

  // Immersive covers the window, so the stable scrollbar gutter must go too;
  // otherwise a strip of background stays visible on the right edge.
  useEffect(() => {
    if (!immersive) return
    document.documentElement.classList.add('player-immersive')
    return () => document.documentElement.classList.remove('player-immersive')
  }, [immersive])

  // Keep refs readable from the player-ready callback without re-creating it.
  useEffect(() => {
    nsfwRevealedRef.current = nsfwRevealed
  }, [nsfwRevealed])

  // Turning protection on while this page is mounted must not leave the video
  // playing behind the gate. Settings is a separate route, so this is only a
  // guard for a same-mount toggle.
  useEffect(() => {
    if (!nsfwMode) return
    nsfwRevealedRef.current = false
    playerRef.current?.stop()
    // oxlint-disable-next-line react/set-state-in-effect
    setPlaying(null)
  }, [nsfwMode])

  // Playback keys (space/K/enter/arrows/volume/mute) live in player-core.js, so
  // the detail dialog gets them too. Only window-level keys belong here.
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
  useEffect(() => {
    tracksRef.current = tracks
  }, [tracks])
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
    updateUiState({ rail: { width: railWidth, collapsed } })
  }, [railWidth, collapsed])

  const onSearch = (value: string) => {
    setSearch(value)
    window.clearTimeout(searchTimer.current)
    searchTimer.current = window.setTimeout(() => {
      setTerm(value.trim())
      setCurrent(-1)
    }, 250)
  }

  const play = useCallback(
    (index: number) => {
      const track = tracks[index]
      if (!track) return
      setCurrent(index)
      setPlaying(track)
      if (!isDesktop) setSnap('half')
      if (nsfwMode && !nsfwRevealedRef.current) {
        pendingPlayRef.current = index
        return
      }
      if (!playerRef.current) {
        pendingPlayRef.current = index
        return
      }
      playerRef.current.setSources(tracks, index)
    },
    [tracks, isDesktop, nsfwMode]
  )

  // A deep link (`/play?movie=12`) resolves once the catalog reaches that movie.
  useEffect(() => {
    appliedTarget.current = null
    lookupAttempts.current = 0
    triedRemote.current = false
  }, [targetMovie])

  useEffect(() => {
    if (!targetMovie || appliedTarget.current === targetMovie) return
    // Wait for the first page: right after switching scope the query is empty
    // and hasNextPage is false, which must not read as "exhausted".
    if (!query.data) return
    const entry = movies.find(candidate => candidate.item.movie_id === targetMovie)
    if (entry) {
      appliedTarget.current = targetMovie
      // Defer so the deep link does not set state during the effect pass.
      window.setTimeout(() => play(entry.start), 0)
      document
        .querySelector(`[data-movie-id="${targetMovie}"]`)
        ?.scrollIntoView({ block: 'nearest' })
      return
    }
    if (!hasNextPage) {
      // A movie opened from the 115 library is not in the local catalog: retry
      // once against the remote scope before giving up.
      if (!triedRemote.current && active.scope === 'local' && pan.data?.connected) {
        triedRemote.current = true
        lookupAttempts.current = 0
        setSourceID('remote')
        return
      }
      appliedTarget.current = targetMovie
      toast.error('当前来源里没有这部影片')
      return
    }
    if (isFetchingNextPage || lookupAttempts.current >= 10) return
    lookupAttempts.current += 1
    void fetchNextPage()
  }, [
    targetMovie,
    query.data,
    movies,
    play,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
    active.scope,
    pan.data?.connected
  ])

  const partIndex = activeMovie ? current - activeMovie.start + 1 : 0
  const movieIndex = movies.findIndex(
    entry => current >= entry.start && current < entry.start + entry.count
  )
  // Manual switching is movie-level; autoplay still walks tracks so multi-part
  // releases continue seamlessly.
  const goMovie = useCallback(
    (delta: number) => {
      if (movieIndex < 0) return
      const entry = movies[movieIndex + delta]
      if (entry) play(entry.start)
    },
    [movieIndex, movies, play]
  )

  // Window-level keys only; playback keys live in player-core.js.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.ctrlKey || event.metaKey || event.altKey) return
      const target = event.target as HTMLElement | null
      if (target && /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)) return
      const key = event.key.toLowerCase()
      if (key === 'f') {
        if (gated || !playing) return
        event.preventDefault()
        playerRef.current?.toggleFullscreen()
      } else if (key === 'n') {
        event.preventDefault()
        goMovie(1)
      } else if (key === 'p') {
        event.preventDefault()
        goMovie(-1)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [gated, playing, goMovie])
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
        // Immersive only shows the video: pin the page to the window edges so
        // no page padding or max-width leaves a gap around the player.
        immersive && 'fixed inset-0 z-40 h-auto w-auto max-w-none gap-0 bg-black p-0'
      )}
    >
      {immersive ? (
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
                aria-label="退出沉浸"
                title="退出沉浸"
                className="text-white hover:bg-white/20 hover:text-white"
                onClick={() => void exitImmersive()}
              >
                <RestoreWindowIcon />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={fullscreen ? '退出全屏' : '全屏'}
                title={fullscreen ? '退出全屏' : '全屏'}
                aria-pressed={fullscreen}
                disabled={gated || !playing}
                className="text-white hover:bg-white/20 hover:text-white"
                onClick={() => playerRef.current?.toggleFullscreen()}
              >
                {fullscreen ? <ExitFullscreenIcon /> : <EnterFullscreenIcon />}
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
          {/* Only the two conflict-free corners resize the frameless window. */}
          <div
            aria-hidden
            className="absolute top-0 left-0 z-[60] size-3 cursor-nwse-resize touch-none"
            onPointerDown={(event: ReactPointerEvent<HTMLDivElement>) => {
              event.preventDefault()
              event.stopPropagation()
              void startWindowResize('NorthWest')
            }}
          />
          <div
            aria-hidden
            className="absolute right-0 bottom-0 z-[60] size-3 cursor-nwse-resize touch-none"
            onPointerDown={(event: ReactPointerEvent<HTMLDivElement>) => {
              event.preventDefault()
              event.stopPropagation()
              void startWindowResize('SouthEast')
            }}
          />
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
          isDesktop && !immersive
            ? ({ '--rail-w': collapsed ? '0px' : `${railWidth}px` } as CSSProperties)
            : undefined
        }
        className={cn(
          'relative grid min-h-0 flex-1 grid-rows-[auto] gap-5 lg:grid-cols-[minmax(0,1fr)_var(--rail-w)] lg:grid-rows-1',
          immersive && 'h-full gap-0 lg:grid-cols-[minmax(0,1fr)]',
          draggingRail
            ? 'lg:transition-none'
            : 'lg:transition-[grid-template-columns] lg:duration-200 lg:ease-out'
        )}
      >
        <section
          ref={playerSectionRef}
          className={cn(
            'flex min-h-0 min-w-0 flex-col gap-3 self-start lg:self-stretch',
            immersive && 'gap-0 self-stretch'
          )}
        >
          <div
            className={cn('relative flex min-h-0 flex-col', (isDesktop || immersive) && 'flex-1')}
          >
            <PlayerStage
              prefs={prefs}
              onFullscreenChange={onFullscreenChange}
              onPrefsChange={next => {
                setPrefs(next)
                savePlayerPrefs(next)
              }}
              className={cn(immersive && 'aspect-auto h-full w-full flex-1 rounded-none')}
              onMetadata={(width, height) => {
                setSourceAspect(width / height)
              }}
              onReady={player => {
                playerRef.current = player
                player.setQueue(tracks)
                player.setControlsHidden(controlsHidden)
                if (pendingPlayRef.current != null && (!nsfwMode || nsfwRevealedRef.current)) {
                  player.setSources(tracksRef.current, pendingPlayRef.current)
                  pendingPlayRef.current = null
                }
              }}
              onChange={(index, source) => {
                setCurrent(index)
                setPlaying(source)
              }}
              onEnded={(_index, hasNext) => {
                if (!hasNext && hasNextPage) void fetchNextPage()
              }}
            />
            {gated ? (
              <div
                className={cn(
                  'absolute inset-0 z-50 grid place-items-center bg-black/90 p-6 text-center',
                  immersive ? 'rounded-none' : 'rounded-2xl'
                )}
              >
                <div className="flex max-w-sm flex-col items-center gap-3 text-white">
                  <LockIcon className="size-8 text-white/75" />
                  <p className="text-sm font-semibold">NSFW 保护已开启</p>
                  <p className="text-xs text-white/60">播放前需要确认</p>
                  <Button size="sm" variant="secondary" disabled={!playing} onClick={revealNsfw}>
                    <PlayIcon className="size-4" />
                    播放
                  </Button>
                </div>
              </div>
            ) : null}
          </div>

          <div className={cn('mt-auto flex min-w-0 items-center gap-3', immersive && 'hidden')}>
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
                  aria-label="沉浸播放"
                  title="沉浸播放"
                  disabled={gated || !playing}
                  onClick={() => void enterImmersive()}
                >
                  <Maximize2Icon />
                </Button>
              ) : null}
              <Button
                variant="outline"
                size="sm"
                disabled={movieIndex <= 0}
                onClick={() => goMovie(-1)}
              >
                上一部
              </Button>
              <Button
                size="sm"
                disabled={movieIndex < 0 || movieIndex + 1 >= movies.length}
                onClick={() => goMovie(1)}
              >
                下一部
              </Button>
            </div>
          </div>
        </section>

        <section
          inert={collapsed && isDesktop && !immersive ? true : undefined}
          style={isDesktop || immersive ? undefined : { height: dragHeight ?? sheetHeight }}
          className={cn(
            'flex min-h-0 flex-col overflow-hidden rounded-2xl bg-card ring-1 ring-border/60 lg:transition-opacity lg:duration-200',
            !immersive &&
              'max-lg:fixed max-lg:inset-x-0 max-lg:bottom-0 max-lg:z-40 max-lg:rounded-b-none max-lg:shadow-[0_-10px_34px_rgba(0,0,0,0.14)]',
            collapsed && isDesktop && !immersive && 'lg:pointer-events-none lg:opacity-0',
            // Immersive keeps the catalog off-screen and slides it in from the
            // right edge, so the video owns the whole frame.
            immersive &&
              cn(
                // Same material as the floating navigation: translucent card
                // with a blur, instead of a gradient edge.
                'fixed inset-y-0 right-0 z-50 w-[20rem] max-w-[80vw] rounded-none rounded-l-2xl border border-border/70 bg-background/85 ring-0 backdrop-blur transition-transform duration-200 ease-out',
                railOpen ? 'translate-x-0' : 'translate-x-full'
              ),
            !immersive &&
              (dragging
                ? 'max-lg:transition-none'
                : 'max-lg:transition-[height] max-lg:duration-200 max-lg:ease-out')
          )}
          onPointerLeave={() => {
            if (immersive) setRailOpen(false)
          }}
        >
          <div
            onPointerDown={onHandleDown}
            onPointerMove={onHandleMove}
            onPointerUp={onHandleUp}
            onPointerCancel={onHandleUp}
            className={cn(
              'flex flex-none cursor-grab touch-none items-center justify-center py-2.5 active:cursor-grabbing lg:hidden',
              immersive && 'hidden'
            )}
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
                      data-movie-id={entry.item.movie_id}
                      onClick={() => play(entry.start)}
                      aria-current={isCurrent ? 'true' : undefined}
                      className={cn(
                        'flex w-full cursor-pointer items-center gap-3 rounded-xl px-2 py-2 text-left transition-colors hover:bg-muted',
                        isCurrent && 'bg-accent ring-1 ring-border/60'
                      )}
                    >
                      <span className="grid aspect-video w-16 flex-none place-items-center overflow-hidden rounded-lg bg-muted text-[0.6rem] text-muted-foreground ring-1 ring-border/60">
                        {entry.item.poster ? (
                          <MediaImage
                            source={entry.item.poster}
                            loading="lazy"
                            className="object-cover"
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

        {isDesktop && !immersive ? (
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

function RestoreWindowIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-4"
      aria-hidden="true"
    >
      <rect x="3" y="5" width="18" height="14" rx="2.5" />
      <path d="M3 9.5h18" />
    </svg>
  )
}

function EnterFullscreenIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-4"
      aria-hidden="true"
    >
      <path d="M9 4H4v5M15 4h5v5M9 20H4v-5M15 20h5v-5" />
    </svg>
  )
}

function ExitFullscreenIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-4"
      aria-hidden="true"
    >
      <path d="M4 9h5V4M20 9h-5V4M4 15h5v5M20 15h-5v5" />
    </svg>
  )
}
