import { createFileRoute } from '@tanstack/react-router'

import { DesktopSection } from '@/features/settings/desktop-section'
import { NetworkSection } from '@/features/settings/network-section'
import { SettingsCard } from '@/features/settings/shared'

export const Route = createFileRoute('/settings/network')({
  component: SettingsNetworkPage
})

function SettingsNetworkPage() {
  return (
    <SettingsCard>
      <NetworkSection />
      <DesktopSection />
    </SettingsCard>
  )
}
