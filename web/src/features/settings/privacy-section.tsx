import { ShieldIcon } from 'lucide-react'

import { Switch } from '@/components/ui/switch'
import { useSettingsStore } from '@/stores/settings'
import { SettingRow, SettingsSection } from './shared'

export function PrivacySection() {
  const nsfwMode = useSettingsStore(state => state.nsfwMode)
  const setNsfwMode = useSettingsStore(state => state.setNsfwMode)

  return (
    <SettingsSection icon={<ShieldIcon className="size-4" />} title="NSFW 保护">
      <SettingRow title="封面隐私模式" description="遮挡封面与预览图；播放前需要确认" inline>
        <Switch checked={nsfwMode} onCheckedChange={setNsfwMode} />
      </SettingRow>
    </SettingsSection>
  )
}
