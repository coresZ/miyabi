import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { apiGet, apiPut } from '@/api/client'

export type JavBusConfig = {
  enabled: boolean
}

export const javbusKeys = {
  config: ['settings', 'javbus'] as const
}

export function useJavBusConfig() {
  return useQuery({
    queryKey: javbusKeys.config,
    queryFn: ({ signal }) => apiGet<JavBusConfig>('/api/settings/javbus', undefined, signal),
    staleTime: 15_000,
    retry: false,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false
  })
}

export function useUpdateJavBusConfig() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (config: JavBusConfig) => apiPut<JavBusConfig>('/api/settings/javbus', config),
    onMutate: async next => {
      await queryClient.cancelQueries({ queryKey: javbusKeys.config })
      const previous = queryClient.getQueryData<JavBusConfig>(javbusKeys.config)
      queryClient.setQueryData<JavBusConfig>(javbusKeys.config, next)
      return { previous }
    },
    onSuccess: next => {
      queryClient.setQueryData(javbusKeys.config, next)
      // Cached movie detail queries carry magnet lists aggregated under the old switch.
      void queryClient.invalidateQueries({ queryKey: ['discover', 'movie'] })
    },
    onError: (_err, _next, context) => {
      if (context?.previous) {
        queryClient.setQueryData(javbusKeys.config, context.previous)
      }
      void queryClient.invalidateQueries({ queryKey: javbusKeys.config })
    }
  })
}
