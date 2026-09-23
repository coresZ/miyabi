import { useState } from 'react'
import { Captions, Globe, Loader2, Minus, Plus, RotateCcw, Search } from 'lucide-react'
import { useMediaPlayer, useMediaState } from '@vidstack/react'

import {
  applySubtitle,
  searchSubtitles,
  updateSubtitleOffset,
  type SubtitleCandidate,
  type SubtitleTrack
} from '@/api/play'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { PlayerTooltipContent } from './tooltip'

export function SubtitleMenu({
  movieID,
  code,
  subtitles,
  activeTrackId,
  onSelectTrack,
  onUpdateTracks
}: {
  movieID: number
  code: string
  subtitles: SubtitleTrack[]
  activeTrackId: number | null
  onSelectTrack: (id: number | null) => void
  onUpdateTracks: (updater: (prev: SubtitleTrack[]) => SubtitleTrack[]) => void
}) {
  const player = useMediaPlayer()
  const controlsVisible = useMediaState('controlsVisible')
  const [open, setOpen] = useState(false)
  const [tooltipOpen, setTooltipOpen] = useState(false)
  const [showSearch, setShowSearch] = useState(false)
  const [searching, setSearching] = useState(false)
  const [candidates, setCandidates] = useState<SubtitleCandidate[]>([])
  const [applyingUrl, setApplyingUrl] = useState<string | null>(null)
  const [offsetBusy, setOffsetBusy] = useState(false)

  const activeTrack = subtitles.find(s => s.id === activeTrackId)

  async function handleSearch() {
    setSearching(true)
    setShowSearch(true)
    try {
      const isUncensored = /uncensored|无码|無碼/i.test(code)
      const results = await searchSubtitles(code, isUncensored)
      setCandidates(results)
    } catch {
      setCandidates([])
    } finally {
      setSearching(false)
    }
  }

  async function handleApplyCandidate(candidate: SubtitleCandidate) {
    setApplyingUrl(candidate.url)
    try {
      const newTrack = await applySubtitle(movieID, candidate)
      onUpdateTracks(prev => {
        const filtered = prev.filter(t => t.id !== newTrack.id)
        return [...filtered, newTrack]
      })
      onSelectTrack(newTrack.id)
      setShowSearch(false)
    } catch (err) {
      console.error('Failed to apply subtitle:', err)
    } finally {
      setApplyingUrl(null)
    }
  }

  async function handleAdjustOffset(deltaMs: number) {
    if (!activeTrack || offsetBusy) return
    const newOffset = activeTrack.offset_ms + deltaMs
    setOffsetBusy(true)

    try {
      await updateSubtitleOffset(activeTrack.id, newOffset)
      onUpdateTracks(prev =>
        prev.map(t => (t.id === activeTrack.id ? { ...t, offset_ms: newOffset } : t))
      )
    } catch (err) {
      console.error('Failed to update subtitle offset:', err)
    } finally {
      setOffsetBusy(false)
    }
  }

  async function handleResetOffset() {
    if (!activeTrack || offsetBusy || activeTrack.offset_ms === 0) return
    setOffsetBusy(true)

    try {
      await updateSubtitleOffset(activeTrack.id, 0)
      onUpdateTracks(prev => prev.map(t => (t.id === activeTrack.id ? { ...t, offset_ms: 0 } : t)))
    } catch (err) {
      console.error('Failed to reset subtitle offset:', err)
    } finally {
      setOffsetBusy(false)
    }
  }

  return (
    <DropdownMenu
      open={open}
      onOpenChange={nextOpen => {
        setOpen(nextOpen)
        if (nextOpen) {
          setTooltipOpen(false)
          player?.controls.pause()
        } else {
          player?.controls.resume()
        }
      }}
    >
      <Tooltip
        open={controlsVisible && tooltipOpen && !open}
        onOpenChange={setTooltipOpen}
        delayDuration={0}
      >
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="字幕设置"
              className={cn(
                'text-white/80 hover:bg-white/10 hover:text-white',
                activeTrackId !== null && 'text-primary hover:text-primary'
              )}
            >
              <Captions className="size-4.5" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <PlayerTooltipContent>字幕设置</PlayerTooltipContent>
      </Tooltip>
      <DropdownMenuContent
        container={player?.el}
        side="top"
        align="end"
        sideOffset={8}
        className="max-h-[30rem] w-80 space-y-1 overflow-y-auto rounded-2xl border border-white/10 bg-zinc-950/95 p-2 text-white shadow-2xl backdrop-blur-2xl"
      >
        {/* Track Selection */}
        <DropdownMenuLabel className="flex items-center justify-between px-2 py-1 text-xs text-white/50">
          <span>字幕轨</span>
          <span>{subtitles.length} 个可用</span>
        </DropdownMenuLabel>

        <DropdownMenuRadioGroup
          value={activeTrackId !== null ? String(activeTrackId) : 'off'}
          onValueChange={val => onSelectTrack(val === 'off' ? null : Number(val))}
          className="max-h-48 space-y-0.5 overflow-y-auto scroll-fade-y pr-1"
        >
          <DropdownMenuRadioItem
            value="off"
            className="cursor-pointer rounded-lg py-1.5 text-white/80 focus:bg-white/10 focus:text-white"
          >
            <span className="text-xs font-medium">关闭字幕</span>
          </DropdownMenuRadioItem>

          {subtitles.map(sub => (
            <DropdownMenuRadioItem
              key={sub.id}
              value={String(sub.id)}
              className="cursor-pointer rounded-lg py-1.5 text-white/80 focus:bg-white/10 focus:text-white"
            >
              <div className="flex min-w-0 flex-col pr-1">
                <span className="truncate text-xs font-medium">{sub.display_name}</span>
                <div className="mt-0.5 flex items-center gap-1.5 text-[10px] text-white/40">
                  <span className="rounded bg-white/10 px-1 py-0.5 text-[9px] uppercase text-white/70">
                    {sub.source === 'local' ? '本地内置' : sub.source}
                  </span>
                  {sub.name ? (
                    <span className="max-w-[130px] truncate text-[10px] text-white/50">
                      {sub.name}
                    </span>
                  ) : null}
                </div>
              </div>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>

        {/* Time Offset Tuning (shown when a track is active) */}
        {activeTrack && (
          <>
            <DropdownMenuSeparator className="my-1 bg-white/10" />
            <div className="space-y-1.5 px-2 py-1">
              <div className="flex items-center justify-between text-xs text-white/60">
                <span>时间轴微调</span>
                <span className="text-[11px] font-medium text-white">
                  {activeTrack.offset_ms > 0 ? '+' : ''}
                  {(activeTrack.offset_ms / 1000).toFixed(1)}s
                </span>
              </div>
              <div className="grid grid-cols-4 gap-1">
                <Button
                  variant="outline"
                  size="xs"
                  className="h-7 border-white/15 bg-white/5 text-[11px] text-white hover:bg-white/15"
                  onClick={() => void handleAdjustOffset(-500)}
                  disabled={offsetBusy}
                >
                  <Minus className="mr-0.5 size-2.5" />
                  0.5s
                </Button>
                <Button
                  variant="outline"
                  size="xs"
                  className="h-7 border-white/15 bg-white/5 text-[11px] text-white hover:bg-white/15"
                  onClick={() => void handleAdjustOffset(500)}
                  disabled={offsetBusy}
                >
                  <Plus className="mr-0.5 size-2.5" />
                  0.5s
                </Button>
                <Button
                  variant="outline"
                  size="xs"
                  className="h-7 border-white/15 bg-white/5 text-[11px] text-white hover:bg-white/15"
                  onClick={() => void handleAdjustOffset(1000)}
                  disabled={offsetBusy}
                >
                  <Plus className="mr-0.5 size-2.5" />
                  1.0s
                </Button>
                <Button
                  variant="outline"
                  size="xs"
                  className="h-7 border-white/15 bg-white/5 text-[11px] text-white hover:bg-white/15"
                  onClick={() => void handleResetOffset()}
                  disabled={offsetBusy || activeTrack.offset_ms === 0}
                >
                  <RotateCcw className="mr-0.5 size-2.5" />
                  重置
                </Button>
              </div>
            </div>
          </>
        )}

        {/* Online Subtitle Search */}
        <DropdownMenuSeparator className="my-1 bg-white/10" />
        <DropdownMenuItem
          onSelect={e => {
            e.preventDefault()
            void handleSearch()
          }}
          className="flex cursor-pointer items-center justify-between rounded-lg py-2 text-white/80 focus:bg-white/10 focus:text-white"
        >
          <div className="flex items-center gap-2">
            <Globe className="size-3.5 text-primary" />
            <span className="text-xs">跨源检索在线字幕</span>
          </div>
          {searching ? (
            <Loader2 className="size-3.5 animate-spin text-primary" />
          ) : (
            <Search className="size-3.5 text-white/40" />
          )}
        </DropdownMenuItem>

        {/* Online Candidates Panel */}
        {showSearch && (
          <div className="space-y-1.5 border-t border-white/10 px-1 pt-1 pb-1">
            {searching ? (
              <div className="flex flex-col items-center justify-center gap-1.5 py-4 text-xs text-white/50">
                <Loader2 className="size-4 animate-spin text-primary" />
                <span>正在跨源聚合检索 (迅雷 + SubtitleCat)…</span>
              </div>
            ) : candidates.length > 0 ? (
              <div className="max-h-48 space-y-1 overflow-y-auto scroll-fade-y pr-1">
                {candidates.map((cand, idx) => {
                  const isApplying = applyingUrl === cand.url
                  return (
                    <div
                      key={idx}
                      className="flex items-center justify-between gap-2 rounded-lg border border-white/5 bg-white/5 p-2 hover:bg-white/10"
                    >
                      <div className="flex min-w-0 flex-col">
                        <span className="truncate text-xs font-medium text-white">
                          {cand.display_name}
                        </span>
                        <div className="mt-0.5 flex items-center gap-1.5 text-[10px] text-white/40">
                          <span className="rounded bg-white/10 px-1 py-0.5 text-[9px] uppercase text-white/70">
                            {cand.source}
                          </span>
                          <span className="max-w-[130px] truncate text-[10px] text-white/50">
                            {cand.name}
                          </span>
                        </div>
                      </div>
                      <Button
                        size="xs"
                        variant="secondary"
                        disabled={isApplying}
                        onClick={() => void handleApplyCandidate(cand)}
                        className="h-6 shrink-0 px-2 text-[10px]"
                      >
                        {isApplying ? <Loader2 className="size-3 animate-spin" /> : '加载'}
                      </Button>
                    </div>
                  )
                })}
              </div>
            ) : (
              <div className="py-3 text-center text-xs text-white/40">
                未检索到匹配的在线字幕
              </div>
            )}
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
