import { createFileRoute } from '@tanstack/react-router'

import { SubscriptionsPage } from '@/features/subscriptions/page'

export const Route = createFileRoute('/subscriptions')({
  component: SubscriptionsPage
})
