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
    onSuccess: next => {
      queryClient.setQueryData(javbusKeys.config, next)
      // Cached movie detail queries carry magnet lists aggregated under the old switch.
      void queryClient.invalidateQueries({ queryKey: ['discover', 'movie'] })
    }
  })
}
