import { MediaImage } from '@/components/media-image'

export function MovieCover({
  source,
  label,
  loading = 'lazy',
  onReady
}: {
  source: string
  /** Movie code for empty/error cover */
  label?: string
  loading?: 'eager' | 'lazy'
  onReady?: () => void
}) {
  return (
    <MediaImage
      source={source}
      fallbackLabel={label}
      loading={loading}
      onReady={onReady}
      blurredBackground
      className="object-contain"
    />
  )
}
