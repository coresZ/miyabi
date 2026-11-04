const PLAYER_CSS = [
  '/player/css/xgplayer.min.css',
  '/player/css/font-awesome.min.css',
  '/player/css/tm-player-controls.css'
]

const PLAYER_JS = [
  '/player/js/xgplayer.min.js',
  '/player/js/xgplayer-flv.min.js',
  '/player/js/hls.min.js',
  '/player/js/tm-player-controls.js',
  '/player/js/gifenc.min.js',
  '/player/js/gif-capture.js',
  '/player/js/player-core.js'
]

function loadCss(href: string): Promise<void> {
  return new Promise(resolve => {
    if (document.querySelector(`link[data-player-asset="${href}"]`)) return resolve()
    const link = document.createElement('link')
    link.rel = 'stylesheet'
    link.href = href
    link.dataset.playerAsset = href
    link.onload = () => resolve()
    link.onerror = () => resolve()
    document.head.appendChild(link)
  })
}

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    if (document.querySelector(`script[data-player-asset="${src}"]`)) return resolve()
    const script = document.createElement('script')
    script.src = src
    script.async = false
    script.dataset.playerAsset = src
    script.onload = () => resolve()
    script.onerror = () => reject(new Error(`播放器资源加载失败：${src}`))
    document.head.appendChild(script)
  })
}

let loading: Promise<void> | null = null

export function loadPlayerAssets(): Promise<void> {
  if (!loading) {
    loading = (async () => {
      await Promise.all(PLAYER_CSS.map(loadCss))
      for (const src of PLAYER_JS) await loadScript(src)
    })()
  }
  return loading
}
