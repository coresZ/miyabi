import { createFileRoute } from '@tanstack/react-router'

import { Separator } from '@/components/ui/separator'
import { LocalSourceSection } from '@/features/settings/local-sources-section'
import { SettingsCard } from '@/features/settings/shared'
import { SourcesSection } from '@/features/sources/sources-section'

export const Route = createFileRoute('/settings/sources')({
  component: SettingsSourcesPage
})

function SettingsSourcesPage() {
  return (
    <SettingsCard>
      <SourcesSection />
      <Separator />
      <LocalSourceSection />
    </SettingsCard>
  )
}
