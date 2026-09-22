import { BellIcon } from 'lucide-react'

import {
  useSubscriptionSettings,
  useUpdateSubscriptionSettings,
  type SubscriptionConfig
} from '@/api/subscriptions'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { SettingRow, SettingsSection } from './shared'

export function SubscriptionSection() {
  const query = useSubscriptionSettings()
  const update = useUpdateSubscriptionSettings()
  const cfg = query.data

  function save(patch: Partial<SubscriptionConfig>) {
    if (!cfg) return
    update.mutate({
      ...cfg,
      ...patch,
      preferences: {
        ...cfg.preferences,
        ...patch.preferences
      }
    })
  }

  const disabled = query.isLoading || query.isError || update.isPending

  return (
    <SettingsSection icon={<BellIcon className="size-4" />} title="订阅与磁力偏好">
      <div className="space-y-6">
        <SettingRow
          inline
          title="影片默认自动推送"
          description="添加影片订阅时默认启用自动下载，出现符合偏好的磁力后自动提交 115 离线下载"
        >
          <Switch
            checked={cfg?.movie_auto_download ?? true}
            disabled={disabled}
            onCheckedChange={checked => save({ movie_auto_download: checked })}
          />
        </SettingRow>

        <SettingRow
          inline
          title="演员新作默认自动推送"
          description="订阅演员后发现新发行的影片时，是否默认直接自动推送到 115"
        >
          <Switch
            checked={cfg?.actor_auto_download ?? false}
            disabled={disabled}
            onCheckedChange={checked => save({ actor_auto_download: checked })}
          />
        </SettingRow>

        <SettingRow
          title="演员新作每日检查时间"
          description="系统每天定时轮询已订阅演员的最新发行动态（默认 04:00）"
        >
          <Input
            type="text"
            className="w-32"
            value={cfg?.actor_check_time ?? '04:00'}
            disabled={disabled}
            placeholder="04:00"
            onChange={event => save({ actor_check_time: event.target.value })}
          />
        </SettingRow>

        <SettingRow
          title="中文字幕偏好"
          description="选择磁力资源时对字幕的容忍度：「必须」无字幕时不入库保持等待；「优先」按字幕加权排序"
        >
          <Select
            value={cfg?.preferences.subtitle ?? 'preferred'}
            disabled={disabled}
            onValueChange={value =>
              save({
                preferences: {
                  subtitle: value as 'preferred' | 'required' | 'any',
                  hd: cfg?.preferences.hd ?? 'preferred',
                  uncensored: cfg?.preferences.uncensored ?? 'any',
                  max_size_gib: cfg?.preferences.max_size_gib ?? 0
                }
              })
            }
          >
            <SelectTrigger className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="preferred">字幕优先</SelectItem>
              <SelectItem value="required">必须有字幕</SelectItem>
              <SelectItem value="any">不限字幕</SelectItem>
            </SelectContent>
          </Select>
        </SettingRow>

        <SettingRow
          title="高清画质偏好"
          description="选择磁力资源时对画质的偏好：「必须」仅限 1080P/4K 高清；「优先」优先选择高清"
        >
          <Select
            value={cfg?.preferences.hd ?? 'preferred'}
            disabled={disabled}
            onValueChange={value =>
              save({
                preferences: {
                  subtitle: cfg?.preferences.subtitle ?? 'preferred',
                  hd: value as 'preferred' | 'required' | 'any',
                  uncensored: cfg?.preferences.uncensored ?? 'any',
                  max_size_gib: cfg?.preferences.max_size_gib ?? 0
                }
              })
            }
          >
            <SelectTrigger className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="preferred">高清优先</SelectItem>
              <SelectItem value="required">必须高清</SelectItem>
              <SelectItem value="any">不限画质</SelectItem>
            </SelectContent>
          </Select>
        </SettingRow>

        <SettingRow
          title="无码/破解资源偏好"
          description="对带有无码破解、流出标签资源的策略：「排除」过滤掉该类磁力"
        >
          <Select
            value={cfg?.preferences.uncensored ?? 'any'}
            disabled={disabled}
            onValueChange={value =>
              save({
                preferences: {
                  subtitle: cfg?.preferences.subtitle ?? 'preferred',
                  hd: cfg?.preferences.hd ?? 'preferred',
                  uncensored: value as 'preferred' | 'required' | 'exclude' | 'any',
                  max_size_gib: cfg?.preferences.max_size_gib ?? 0
                }
              })
            }
          >
            <SelectTrigger className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="any">不限</SelectItem>
              <SelectItem value="preferred">无码优先</SelectItem>
              <SelectItem value="exclude">排除无码/破解</SelectItem>
              <SelectItem value="required">必须无码/破解</SelectItem>
            </SelectContent>
          </Select>
        </SettingRow>

        <SettingRow
          title="单部资源体积上限 (GiB)"
          description="过滤超过指定大小的磁力，避免单任务占用过大网盘空间；设置为 0 表示不限制"
        >
          <Input
            type="number"
            min={0}
            className="w-32"
            value={cfg?.preferences.max_size_gib ?? 0}
            disabled={disabled}
            onChange={event => {
              const val = Number(event.target.value)
              if (!Number.isNaN(val) && val >= 0) {
                save({
                  preferences: {
                    subtitle: cfg?.preferences.subtitle ?? 'preferred',
                    hd: cfg?.preferences.hd ?? 'preferred',
                    uncensored: cfg?.preferences.uncensored ?? 'any',
                    max_size_gib: val
                  }
                })
              }
            }}
          />
        </SettingRow>
      </div>
    </SettingsSection>
  )
}
