import { useState } from 'react'
import { GlobeIcon, RefreshCwIcon } from 'lucide-react'
import { toast } from 'sonner'

import {
  type NetworkProbeResult,
  type NetworkTestResponse,
  useNetworkConfig,
  useTestNetwork,
  useUpdateNetworkConfig
} from '@/api/network'
import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { SettingRow, SettingsSection } from './shared'

export function NetworkSection() {
  const network = useNetworkConfig()
  const updateConfig = useUpdateNetworkConfig()
  const testNetwork = useTestNetwork()

  const config = network.data
  const [userInput, setUserInput] = useState<string | null>(null)

  const url = userInput ?? config?.url ?? ''
  const isDirty = userInput !== null && userInput !== (config?.url ?? '')
  const isEnabled = config?.enabled ?? false
  const isJavBusEnabled = config?.javbus_enabled ?? false
  const disabled = network.isLoading || network.isError || updateConfig.isPending

  function save(enabled: boolean, successMessage: string) {
    updateConfig.mutate(
      { enabled, url: normalizeProxyInput(url), javbus_enabled: isJavBusEnabled },
      {
        onSuccess: () => {
          setUserInput(null)
          toast.success(successMessage)
        },
        onError: error => {
          toast.error(error instanceof Error ? error.message : '保存网络代理失败')
        }
      }
    )
  }

  function saveJavBus(enabled: boolean) {
    updateConfig.mutate(
      { enabled: isEnabled, url: normalizeProxyInput(url), javbus_enabled: enabled },
      {
        onSuccess: () => {
          toast.success(enabled ? '已开启 JavBus 数据源' : '已关闭 JavBus 数据源')
          if (enabled) {
            handleTest()
          }
        },
        onError: error => {
          toast.error(error instanceof Error ? error.message : '保存 JavBus 设置失败')
        }
      }
    )
  }

  function handleSave() {
    if (isDirty) save(isEnabled, '代理地址已保存')
  }

  function handleTest() {
    testNetwork.mutate(
      { enabled: true, url: normalizeProxyInput(url), javbus_enabled: isJavBusEnabled },
      {
        onSuccess: showProbeResult,
        onError: error => {
          toast.error(error instanceof Error ? error.message : '连通性测试失败')
        }
      }
    )
  }

  return (
    <SettingsSection icon={<GlobeIcon className="size-4" />} title="网络代理">
      <SettingRow title="代理服务" description="仅为 JavDB 与 JavBus 提供网络代理" inline>
        <Switch
          checked={isEnabled}
          disabled={disabled}
          onCheckedChange={enabled => save(enabled, enabled ? '已开启网络代理' : '已关闭网络代理')}
        />
      </SettingRow>

      <SettingRow
        title="JavBus 数据源"
        description="启用 JavBus 磁力聚合与画质标签推断（需代理）"
        inline
      >
        <Switch checked={isJavBusEnabled} disabled={disabled} onCheckedChange={saveJavBus} />
      </SettingRow>

      {isEnabled ? (
        <>
          <SettingRow title="代理地址" description="支持 HTTP、HTTPS 与 SOCKS5 代理协议">
            <div className="flex w-full items-center gap-2 sm:w-auto">
              <Input
                type="text"
                value={url}
                placeholder="http://127.0.0.1:7890"
                className="w-full sm:w-80"
                disabled={disabled}
                onChange={e => setUserInput(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') handleSave()
                }}
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={disabled || !isDirty}
                onClick={handleSave}
              >
                保存
              </Button>
            </div>
          </SettingRow>

          <SettingRow title="连通性测试" description="测试当前填写的代理地址网络连通状态" inline>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled || testNetwork.isPending}
              onClick={handleTest}
            >
              <RefreshCwIcon className={cn('size-3.5', testNetwork.isPending && 'animate-spin')} />
              测试连接
            </Button>
          </SettingRow>
        </>
      ) : null}

      {network.isError ? (
        <InlineError>
          后端服务暂不可用，无法读取网络设置。请检查后端服务状态后刷新重试。
        </InlineError>
      ) : null}
    </SettingsSection>
  )
}

function showProbeResult({ javdb, javbus }: NetworkTestResponse) {
  const description = `JavDB ${describeProbe(javdb)}，JavBus ${describeProbe(javbus)}`
  const available = Number(javdb.available) + Number(javbus.available)
  if (available === 2) toast.success('代理连接正常', { description })
  else if (available === 1) toast.warning('部分站点无法连接', { description })
  else toast.error('代理连接失败', { description })
}

function describeProbe(result: NetworkProbeResult): string {
  return result.available ? `${result.latency_ms ?? 0} ms` : '不可用'
}

function normalizeProxyInput(input: string): string {
  const trimmed = input.trim()
  if (!trimmed || /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(trimmed)) return trimmed
  return `http://${trimmed}`
}
