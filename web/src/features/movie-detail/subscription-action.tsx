import { BellOffIcon, BellPlusIcon, BellRingIcon, LoaderCircleIcon } from 'lucide-react'

import { useAddSubscription, useRemoveSubscription, useSubscription } from '@/api/subscriptions'
import { Button } from '@/components/ui/button'

// Inline subscription toggle for the detail page when no magnet exists yet.
export function MovieSubscriptionAction({ movieID }: { movieID: string }) {
  const { subscription, isPending } = useSubscription('movie', movieID)
  const add = useAddSubscription()
  const remove = useRemoveSubscription()
  const subscribed = subscription?.status === 'waiting'
  const busy = isPending || add.isPending || remove.isPending

  return (
    <div className="flex flex-wrap items-center gap-3">
      <p className="text-sm text-muted-foreground">
        {subscribed
          ? '已订阅，出现磁力后会自动加入 115。'
          : subscription?.status === 'added'
            ? '已成功加入 115 离线下载。'
            : '暂无磁力链'}
      </p>
      <Button
        type="button"
        variant={subscribed ? 'outline' : 'default'}
        size="sm"
        disabled={busy}
        onClick={() =>
          subscribed
            ? remove.mutate(subscription.id)
            : add.mutate({ kind: 'movie', target_id: movieID })
        }
      >
        {add.isPending || remove.isPending ? (
          <LoaderCircleIcon className="animate-spin" />
        ) : subscribed ? (
          <BellOffIcon />
        ) : subscription ? (
          <BellRingIcon />
        ) : (
          <BellPlusIcon />
        )}
        {subscribed ? '取消订阅' : subscription ? '重新订阅' : '订阅影片'}
      </Button>
    </div>
  )
}
