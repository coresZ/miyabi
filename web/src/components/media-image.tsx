import { ImageIcon, ImageOffIcon } from 'lucide-react'
import { useEffect, useState } from 'react'

import { imageURL } from '@/api/client'
import { cn } from 'cn'
import { useSettingsStore } from '@/stores/settings'

type MediaImageProps = {
  source: string
  original?: string
  /** Shown when missing/failed/concealed — typically movie code */
  fallbackLabel?: string
  loading?: 'eager' | 'lazy'
  blurredBackground?: boolean
  onReady?: () => void
  onImageLoad?: (image: HTMLImageElement) => void
  className?: string
}

export function MediaImage(props: MediaImageProps) {
  const nsfwMode = useSettingsStore(state => state.nsfwMode)
  return (
    <MediaImageContent
      key={JSON.stringify([props.source, props.original, nsfwMode, props.fallbackLabel])}
      {...props}
      concealed={nsfwMode}
    />
  )
}

function MediaImageContent({
  source,
  original,
  fallbackLabel,
  loading = 'lazy',
  blurredBackground = false,
  onReady,
  onImageLoad,
  concealed,
  className
}: MediaImageProps & { concealed: boolean }) {
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading')
  const hasSource = source.length > 0
  const showImage = !concealed && hasSource && status !== 'error'
  const ready = !showImage || status === 'ready'
  const showError = !concealed && hasSource && status === 'error'
  const showEmpty = !concealed && !hasSource

  useEffect(() => {
    if (ready) onReady?.()
  }, [ready, onReady])

  const imageProps = {
    src: imageURL(source),
    srcSet: original ? `${imageURL(source)} 1x, ${imageURL(original)} 2x` : undefined,
    loading,
    decoding: 'async',
    referrerPolicy: 'no-referrer'
  } as const

  return (
    <div className="relative size-full overflow-hidden bg-muted">
      <ImagePlaceholder
        label={fallbackLabel}
        mode={concealed ? 'concealed' : showError ? 'error' : showEmpty ? 'empty' : 'loading'}
      />
      {showImage ? (
        <div className={cn('absolute inset-0', !ready && 'opacity-0')}>
          {blurredBackground ? (
            <img
              {...imageProps}
              alt=""
              className="pointer-events-none absolute inset-0 size-full scale-125 object-cover blur-xl brightness-75"
            />
          ) : null}
          <img
            {...imageProps}
            alt=""
            className={cn('relative size-full', className)}
            onLoad={async event => {
              const image = event.currentTarget
              onImageLoad?.(image)
              try {
                if (typeof image.decode === 'function') await image.decode()
                setStatus('ready')
              } catch {
                setStatus('error')
              }
            }}
            onError={() => setStatus('error')}
          />
        </div>
      ) : null}
    </div>
  )
}

function ImagePlaceholder({
  label,
  mode
}: {
  label?: string
  mode: 'loading' | 'empty' | 'error' | 'concealed'
}) {
  const text = label?.trim()
  return (
    <div
      className={cn(
        'flex size-full flex-col items-center justify-center gap-2 bg-gradient-to-br from-muted to-muted/60 px-3 text-muted-foreground',
        mode === 'error' && 'from-muted to-destructive/5',
        mode === 'concealed' && 'from-muted to-secondary'
      )}
    >
      {text ? (
        <span className="line-clamp-3 text-center text-sm font-semibold tracking-wide break-all text-foreground/70">
          {text}
        </span>
      ) : mode === 'error' ? (
        <ImageOffIcon className="size-6 opacity-70" />
      ) : (
        <ImageIcon className="size-6 opacity-50" />
      )}
      {mode === 'error' && text ? (
        <span className="text-[10px] text-muted-foreground">封面加载失败</span>
      ) : null}
      {mode === 'concealed' ? (
        <span className="text-[10px] text-muted-foreground">隐私模式</span>
      ) : null}
    </div>
  )
}
