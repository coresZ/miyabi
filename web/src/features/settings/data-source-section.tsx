import {
  CheckCircle2Icon,
  DatabaseIcon,
  LoaderCircleIcon,
  RefreshCwIcon,
  XCircleIcon
} from 'lucide-react'
import { toast } from 'sonner'

import {
  type JavDBRouteCandidate,
  useJavDBRoute,
  useReselectJavDBRoute,
  useSelectJavDBRoute
} from '@/api/discover'
import { useJavBusConfig, useUpdateJavBusConfig } from '@/api/javbus'
import { useNetworkConfig } from '@/api/network'
import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { SettingRow, SettingsSection } from './shared'

const AUTO_ROUTE_VALUE = '__auto__'

export function DataSourceSection() {
  // JavDB
  const route = useJavDBRoute()
  const reselect = useReselectJavDBRoute()
  const selectRoute = useSelectJavDBRoute()
  const status = route.data
  const value = status?.manual ? status.host : AUTO_ROUTE_VALUE
  const activeCandidate = status?.candidates.find(candidate => candidate.host === status.host)
  const busy = route.isFetching || reselect.isPending || selectRoute.isPending

  // JavBus
  const javbusConfig = useJavBusConfig()
  const updateJavbus = useUpdateJavBusConfig()
  const network = useNetworkConfig()

  const javbusEnabled = javbusConfig.data?.enabled ?? false
  const isProxyEnabled = Boolean(network.data?.enabled)
  const javbusDisabled = javbusConfig.isLoading || javbusConfig.isError || network.isLoading

  function changeRoute(next: string) {
    reselect.reset()
    selectRoute.mutate(next === AUTO_ROUTE_VALUE ? '' : next)
  }

  function refreshRoutes() {
    selectRoute.reset()
    if (route.isError) {
      reselect.reset()
      void route.refetch()
    } else {
      reselect.mutate()
    }
  }

  function handleJavbusToggle(next: boolean) {
    if (next && !isProxyEnabled) {
      toast.error('请先开启网络代理以使用 JavBus 数据源')
      return
    }

    updateJavbus.mutate(
      { enabled: next },
      {
        onSuccess: saved =>
          toast.success(saved.enabled ? '已开启 JavBus 磁力源' : '已关闭 JavBus 磁力源'),
        onError: error =>
          toast.error(error instanceof Error ? error.message : '保存 JavBus 设置失败')
      }
    )
  }

  return (
    <SettingsSection icon={<DatabaseIcon className="size-4" />} title="数据源">
      <SettingRow
        title="JavDB 接口线路"
        description="自动优选或手动选择线路，连接失败时自动重选"
      >
        <div className="flex w-full items-center gap-2 sm:w-auto">
          <Select
            value={value}
            disabled={busy || !status || route.isError}
            onValueChange={changeRoute}
          >
            <SelectTrigger className="min-w-0 flex-1 sm:w-72">
              <SelectValue>
                {route.isError ? (
                  '后端不可用'
                ) : route.isPending ? (
                  '正在读取线路'
                ) : value === AUTO_ROUTE_VALUE ? (
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="text-xs">自动优选</span>
                    {status?.host ? (
                      <span className="hidden truncate text-xs text-muted-foreground sm:inline">
                        {formatHost(status.host)}
                      </span>
                    ) : null}
                  </span>
                ) : (
                  <RouteDisplay host={value} candidate={activeCandidate} />
                )}
              </SelectValue>
            </SelectTrigger>
            <SelectContent position="popper" align="end">
              <SelectGroup>
                <SelectItem value={AUTO_ROUTE_VALUE}>自动优选</SelectItem>
                {status?.candidates.map(candidate => (
                  <SelectItem key={candidate.host} value={candidate.host}>
                    <RouteDisplay host={candidate.host} candidate={candidate} />
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                variant="outline"
                size="icon"
                disabled={busy}
                onClick={refreshRoutes}
              >
                <RefreshCwIcon className={cn('size-4', busy && 'animate-spin')} />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{route.isError ? '重试' : '重新测速'}</TooltipContent>
          </Tooltip>
        </div>
      </SettingRow>

      {route.isError ? (
        <InlineError>
          后端服务暂不可用，请启动后端服务后点击重试。
        </InlineError>
      ) : selectRoute.isError || reselect.isError ? (
        <InlineError>
          {selectRoute.isError
            ? '线路切换未完成，请稍后重试。'
            : '暂时没有找到可用线路，请稍后重新测速。'}
        </InlineError>
      ) : status && !status.active ? (
        <p className="text-xs text-muted-foreground">
          尚无缓存线路，首次请求时将完成全部线路测速。
        </p>
      ) : null}

      <SettingRow
        title="JavBus 磁力源"
        description="开启后磁力列表将合并 JavBus 的资源（需要网络代理）"
        inline
      >
        <Switch
          checked={javbusEnabled}
          disabled={javbusDisabled}
          onCheckedChange={handleJavbusToggle}
        />
      </SettingRow>
      {javbusConfig.isError ? (
        <InlineError>后端服务暂不可用，无法读取 JavBus 设置。</InlineError>
      ) : null}
    </SettingsSection>
  )
}

function RouteDisplay({ host, candidate }: { host: string; candidate?: JavDBRouteCandidate }) {
  return (
    <span className="flex w-full min-w-0 items-center justify-between gap-2">
      <span className="truncate">{formatHost(host)}</span>
      {candidate?.status === 'available' ? (
        <span
          className={cn(
            'inline-flex shrink-0 items-center gap-1 text-xs',
            latencyTone(candidate.latency_ms)
          )}
        >
          <CheckCircle2Icon className="size-3" />
          {candidate.latency_ms} ms
        </span>
      ) : candidate?.status === 'unavailable' ? (
        <span className="inline-flex shrink-0 items-center gap-1 text-xs text-destructive">
          <XCircleIcon className="size-3" />
          不可用
        </span>
      ) : (
        <span className="inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
          <LoaderCircleIcon className="size-3" />
          未测速
        </span>
      )}
    </span>
  )
}

function formatHost(host: string) {
  return host.replace(/^https?:\/\//, '')
}

function latencyTone(latencyMS: number) {
  if (latencyMS <= 500) return 'text-success'
  if (latencyMS <= 1500) return 'text-warning'
  return 'text-orange-600 dark:text-orange-400'
}
