import { MediaImage } from '@/components/media-image'

export function MovieCover({
  source,
  label,
  loading = 'lazy'
}: {
  source: string
  /** Movie code for empty/error cover */
  label?: string
  loading?: 'eager' | 'lazy'
}) {
  return (
    <MediaImage
      source={source}
      fallbackLabel={label}
      loading={loading}
      blurredBackground
      className="object-contain"
    />
  )
}
