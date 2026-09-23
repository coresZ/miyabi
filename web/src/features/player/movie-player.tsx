import { useId, useState } from 'react'

import { usePlayFiles, type PlayFiles } from '@/api/play'
import { PlayerError, PlayerLoading } from './player-status'
import { PlaybackPlayer } from './playback-player'
import { watchSessions } from './watch-progress-writer'

import '@vidstack/react/player/styles/default/theme.css'
import '@vidstack/react/player/styles/default/layouts/video.css'
import './player.css'

export default function MoviePlayer({ movieID }: { movieID: number }) {
  const openingID = useId()
  const files = usePlayFiles(movieID, openingID)

  if (files.isPending) return <PlayerLoading />
  if (files.isError) {
    return <PlayerError error={files.error} onRetry={() => void files.refetch()} />
  }
  return (
    <MoviePlayback
      key={files.dataUpdatedAt}
      movieID={movieID}
      openingID={openingID}
      files={files.data}
      onRetry={() => void files.refetch()}
    />
  )
}

function MoviePlayback({
  movieID,
  openingID,
  files,
  onRetry
}: {
  movieID: number
  openingID: string
  files: PlayFiles
  onRetry: () => void
}) {
  // Freeze file selection and resume for this opening. A late watch write must
  // never switch files or seek backwards after playback has already started.
  const [{ file, history, source, title }] = useState(() => {
    const history = watchSessions.resume(movieID, files.source, files.resume)
    return {
      history,
      source: files.source,
      title: files.title,
      file: files.files.find(item => item.id === history?.file_id) ?? files.files[0]
    }
  })
  if (!file) {
    return (
      <PlayerError title={title} message="没有可播放的文件，请重新扫描媒体库。" onRetry={onRetry} />
    )
  }

  return (
    <PlaybackPlayer
      title={title}
      code={files.code}
      movieID={movieID}
      openingID={openingID}
      source={source}
      fileID={file.id}
      history={history}
      initialSubtitles={files.subtitles}
    />
  )
}
