import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { ApiError, apiDelete, apiGet, apiPatch, apiPost, apiPut } from '@/api/client'
import type { MovieState } from '@/api/discover'

export type SubscriptionKind = 'movie' | 'actor'

export type SubscriptionStatus = 'waiting' | 'added' | 'stale' | 'active' | 'paused' | 'error'

export type SubscriptionItem = {
  id: number
  kind: SubscriptionKind
  target_id: string
  code: string
  title: string
  cover: string
  release_date?: string
  origin_id?: number
  auto_download: boolean
  zone?: string
  status: SubscriptionStatus
  cursor?: string
  hash?: string
  task_id?: number
  next_check_at?: string
  last_checked_at?: string
  checks: number
  error?: string
  state?: MovieState
  library_id?: number
  created_at: string
  updated_at: string
}

export type MagnetPreferences = {
  subtitle: 'preferred' | 'required' | 'any'
  hd: 'preferred' | 'required' | 'any'
  uncensored: 'preferred' | 'required' | 'exclude' | 'any'
  max_size_gib: number
}

export type SubscriptionConfig = {
  movie_auto_download: boolean
  actor_auto_download: boolean
  actor_check_time: string
  preferences: MagnetPreferences
}

export const subscriptionKeys = {
  all: ['subscriptions'] as const,
  list: (kind?: string) => ['subscriptions', 'list', kind] as const,
  feed: (actorID: number) => ['subscriptions', 'feed', actorID] as const,
  settings: ['subscriptions', 'settings'] as const
}

function describeError(error: unknown) {
  return error instanceof ApiError ? error.message : '请检查后端服务和网络后重试。'
}

export function useSubscriptions(kind?: SubscriptionKind, enabled = true) {
  return useQuery({
    queryKey: subscriptionKeys.list(kind),
    queryFn: ({ signal }) => {
      const search = kind ? `?kind=${encodeURIComponent(kind)}&limit=100` : '?limit=100'
      return apiGet<SubscriptionItem[]>(`/api/subscriptions${search}`, undefined, signal)
    },
    enabled,
    staleTime: 30_000,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    retry: false
  })
}

export function useSubscription(kind: SubscriptionKind, targetID: string) {
  const subscriptions = useSubscriptions(kind)
  return {
    subscription: subscriptions.data?.find(item => item.target_id === targetID),
    isPending: subscriptions.isPending
  }
}

export function useAddSubscription() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload: {
      kind: SubscriptionKind
      target_id: string
      title?: string
      cover?: string
      auto_download?: boolean
      zone?: string
    }) => apiPost<SubscriptionItem>('/api/subscriptions', payload),
    retry: false,
    onSuccess: item => {
      queryClient.setQueryData<SubscriptionItem[]>(subscriptionKeys.list(item.kind), items => [
        item,
        ...(items ?? []).filter(existing => existing.target_id !== item.target_id)
      ])
      void queryClient.invalidateQueries({ queryKey: subscriptionKeys.all })
      const text =
        item.kind === 'actor'
          ? `已订阅演员「${item.title}」`
          : `已订阅「${item.code || item.title}」`
      toast.success(text, { description: '有新磁力或新作发布时将自动提醒或推送到 115。' })
    },
    onError: error => toast.error('添加订阅失败', { description: describeError(error) })
  })
}

export function useUpdateSubscription() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({
      id,
      ...patch
    }: {
      id: number
      auto_download?: boolean
      zone?: string
      status?: SubscriptionStatus
    }) => apiPatch<SubscriptionItem>(`/api/subscriptions/${id}`, patch),
    retry: false,
    onSuccess: item => {
      queryClient.setQueryData<SubscriptionItem[]>(subscriptionKeys.list(item.kind), items =>
        (items ?? []).map(existing => (existing.id === item.id ? item : existing))
      )
      void queryClient.invalidateQueries({ queryKey: subscriptionKeys.all })
      toast.success('订阅已更新')
    },
    onError: error => toast.error('更新订阅失败', { description: describeError(error) })
  })
}

export function useRemoveSubscription() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<null>(`/api/subscriptions/${id}`),
    retry: false,
    onSuccess: (_, id) => {
      queryClient.setQueryData<SubscriptionItem[]>(subscriptionKeys.list('movie'), items =>
        (items ?? []).filter(item => item.id !== id)
      )
      queryClient.setQueryData<SubscriptionItem[]>(subscriptionKeys.list('actor'), items =>
        (items ?? []).filter(item => item.id !== id)
      )
      void queryClient.invalidateQueries({ queryKey: subscriptionKeys.all })
      toast.success('已取消订阅')
    },
    onError: error => toast.error('取消订阅失败', { description: describeError(error) })
  })
}

export function useEnqueueSubscription() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiPost<SubscriptionItem>(`/api/subscriptions/${id}/enqueue`),
    retry: false,
    onSuccess: item => {
      queryClient.setQueryData<SubscriptionItem[]>(subscriptionKeys.list(item.kind), items =>
        (items ?? []).map(existing => (existing.id === item.id ? item : existing))
      )
      void queryClient.invalidateQueries({ queryKey: subscriptionKeys.all })
      if (item.status === 'added') {
        toast.success(item.code || item.title, { description: '已成功提交到 115 离线下载。' })
      } else {
        toast.info(item.code || item.title, {
          description: '暂无匹配磁力，已开启自动下载，出种后将自动推送。'
        })
      }
    },
    onError: error => toast.error('入库失败', { description: describeError(error) })
  })
}

export function useBatchEnqueueSubscriptions() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload: { ids?: number[]; all?: boolean }) =>
      apiPost<{ task_id: number }>('/api/subscriptions/enqueue', payload),
    retry: false,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: subscriptionKeys.all })
      toast.success('批量入库任务已创建', {
        description: '正在后台队列安全入库，每部之间随机间隔防风控。'
      })
    },
    onError: error => toast.error('创建批量入库任务失败', { description: describeError(error) })
  })
}

export function useActorFeed(actorID: number, enabled = true) {
  return useQuery({
    queryKey: subscriptionKeys.feed(actorID),
    queryFn: ({ signal }) =>
      apiGet<SubscriptionItem[]>(
        `/api/subscriptions/actors/${actorID}/feed?limit=100`,
        undefined,
        signal
      ),
    enabled: enabled && actorID > 0,
    staleTime: 30_000,
    refetchOnWindowFocus: false,
    retry: false
  })
}

export function useSubscriptionSettings() {
  return useQuery({
    queryKey: subscriptionKeys.settings,
    queryFn: ({ signal }) =>
      apiGet<SubscriptionConfig>('/api/settings/subscription', undefined, signal),
    staleTime: 60_000,
    refetchOnWindowFocus: false,
    retry: false
  })
}

export function useUpdateSubscriptionSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (cfg: SubscriptionConfig) =>
      apiPut<SubscriptionConfig>('/api/settings/subscription', cfg),
    retry: false,
    onSuccess: cfg => {
      queryClient.setQueryData<SubscriptionConfig>(subscriptionKeys.settings, cfg)
      toast.success('订阅设置已保存')
    },
    onError: error => toast.error('保存设置失败', { description: describeError(error) })
  })
}
