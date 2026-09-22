import { DatabaseIcon } from 'lucide-react'
import { toast } from 'sonner'

import { useJavBusConfig, useUpdateJavBusConfig } from '@/api/javbus'
import { InlineError } from '@/components/error-state'
import { Switch } from '@/components/ui/switch'
import { SettingRow, SettingsSection } from './shared'

export function JavBusSection() {
  const config = useJavBusConfig()
  const update = useUpdateJavBusConfig()
  const enabled = config.data?.enabled ?? false
  const disabled = config.isLoading || config.isError || update.isPending

  return (
    <SettingsSection icon={<DatabaseIcon className="size-4" />} title="JavBus">
      <SettingRow
        title="磁力数据源"
        description="开启后磁力列表合并 JavBus 的资源，按来源加徽章；通常需要网络代理"
        inline
      >
        <Switch
          checked={enabled}
          disabled={disabled}
          onCheckedChange={next =>
            update.mutate(
              { enabled: next },
              {
                onSuccess: saved =>
                  toast.success(saved.enabled ? '已开启 JavBus 数据源' : '已关闭 JavBus 数据源'),
                onError: error =>
                  toast.error(error instanceof Error ? error.message : '保存 JavBus 设置失败')
              }
            )
          }
        />
      </SettingRow>
      {config.isError ? <InlineError>后端服务暂不可用，无法读取 JavBus 设置。</InlineError> : null}
    </SettingsSection>
  )
}
