import { ShieldIcon } from 'lucide-react'

import { Switch } from '@/components/ui/switch'
import { updateUiState, useUiState } from '@/stores/ui-state'
import { SettingRow, SettingsSection } from './shared'

export function PrivacySection() {
  const nsfwMode = useUiState(state => state.nsfwMode)

  return (
    <SettingsSection icon={<ShieldIcon className="size-4" />} title="NSFW 保护">
      <SettingRow title="封面隐私模式" description="遮挡封面与预览图；播放前需要确认" inline>
        <Switch
          checked={nsfwMode}
          onCheckedChange={enabled => updateUiState({ nsfwMode: enabled })}
        />
      </SettingRow>
    </SettingsSection>
  )
}
