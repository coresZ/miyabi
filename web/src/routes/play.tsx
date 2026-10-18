import { createFileRoute } from '@tanstack/react-router'

import { PlayerPage } from '@/features/player/page'

export const Route = createFileRoute('/play')({
  component: PlayerPage
})
