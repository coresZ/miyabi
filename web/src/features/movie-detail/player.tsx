import { PlayIcon } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'

import type { LocalVideo } from '@/api/library'
import { playableVideoUrl } from '@/api/playables'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { PlayerStage, type PlayerInstance, type PlayerTrack } from '@/features/player/player-stage'

export function MoviePlayer({ videos, title }: { videos: LocalVideo[]; title: string }) {
  const [open, setOpen] = useState(false)
  const [index, setIndex] = useState(0)
  const playerRef = useRef<PlayerInstance | null>(null)
  const tracks = useMemo<PlayerTrack[]>(
    () =>
      videos.map(video => ({ url: playableVideoUrl('local', video.file_id), name: video.name })),
    [videos]
  )
  const current = videos[Math.min(index, videos.length - 1)]
  if (!current) return null

  const play = (target: number) => {
    setIndex(target)
    playerRef.current?.setSources(tracks, target)
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        <PlayIcon />
        播放
      </Button>
      <DialogContent
        className="dark flex h-[min(52rem,calc(100dvh-2rem))] w-[calc(100%-2rem)] max-w-[calc(100%-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-6xl"
        onCloseAutoFocus={event => event.preventDefault()}
        onInteractOutside={event => event.preventDefault()}
      >
        <DialogTitle className="sr-only">{title || current.name}</DialogTitle>
        <PlayerStage
          className="aspect-auto min-h-0 flex-1 rounded-none"
          onReady={player => {
            playerRef.current = player
            player.setSources(tracks, index)
          }}
          onChange={itemIndex => setIndex(itemIndex)}
        />
        {videos.length > 1 ? (
          <div className="flex flex-wrap gap-2 border-t border-border/60 p-3">
            {videos.map((video, itemIndex) => (
              <Button
                key={video.file_id}
                variant={itemIndex === index ? 'secondary' : 'ghost'}
                size="sm"
                onClick={() => play(itemIndex)}
              >
                {video.name}
              </Button>
            ))}
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
