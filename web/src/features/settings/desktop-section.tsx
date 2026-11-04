import { MonitorIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import { InlineError } from '@/components/error-state'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { applyDesktop, desktopConfig, isDesktopShell, type DesktopConfig } from './desktop-bridge'
import { SettingRow, SettingsSection } from './shared'

export function DesktopSection() {
  if (!isDesktopShell()) return null
  return (
    <>
      <Separator />
      <DesktopSettings />
    </>
  )
}

function DesktopSettings() {
  const [config, setConfig] = useState<DesktopConfig | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [lan, setLan] = useState(false)
  const [port, setPort] = useState('8080')
  const [password, setPassword] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  useEffect(() => {
    let active = true
    void desktopConfig()
      .then(next => {
        if (!active) return
        setConfig(next)
        setLan(next.lan)
        setPort(String(next.port))
      })
      .catch(err => {
        if (!active) return
        setError(err instanceof Error ? err.message : '无法读取桌面设置')
      })
    return () => {
      active = false
    }
  }, [])

  if (error) {
    return (
      <SettingsSection icon={<MonitorIcon className="size-4" />} title="桌面">
        <InlineError>{error}</InlineError>
      </SettingsSection>
    )
  }
  if (!config) return null

  const portNumber = Number(port)
  const passwordDirty = password !== null && password !== ''
  const cleared = password === ''
  const dirty = lan !== config.lan || (lan && portNumber !== config.port) || passwordDirty || cleared
  const effectivePassword = password === null ? (config.hasPassword ? 'set' : '') : password.trim()

  function save() {
    if (lan && !effectivePassword) {
      toast.error('开启局域网访问需要设置访问密码')
      return
    }
    if (!Number.isInteger(portNumber) || portNumber < 1 || portNumber > 65535) {
      toast.error('端口无效')
      return
    }
    setPending(true)
    void applyDesktop({ lan, port: portNumber, password })
      .catch(err => {
        setPending(false)
        toast.error(err instanceof Error ? err.message : '无法重启')
      })
  }

  return (
    <SettingsSection icon={<MonitorIcon className="size-4" />} title="桌面">
      <SettingRow title="数据目录" description="在首次启动时设定，不能在这里修改。">
        <div className="max-w-sm text-sm break-all text-muted-foreground">{config.dataDir}</div>
      </SettingRow>
      <SettingRow title="局域网访问" description="关闭时只有本机窗口能访问。开启后同网段设备可用浏览器打开下面的地址。" inline>
        <Switch checked={lan} disabled={pending} onCheckedChange={setLan} />
      </SettingRow>
      {lan ? (
        <SettingRow title="端口" description="保存后重启服务生效。">
          <Input
            type="number"
            min={1}
            max={65535}
            value={port}
            disabled={pending}
            onChange={event => setPort(event.target.value)}
          />
        </SettingRow>
      ) : null}
      <SettingRow title="访问密码" description="开启局域网时必须设置。留空表示不修改。">
        <div className="flex items-center gap-2">
          <Input
            type="password"
            value={password ?? ''}
            placeholder="留空表示不修改"
            autoComplete="new-password"
            disabled={pending}
            onChange={event => setPassword(event.target.value)}
          />
          {config.hasPassword ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={pending}
              onClick={() => setPassword('')}
            >
              清除密码
            </Button>
          ) : null}
        </div>
      </SettingRow>
      <SettingRow title="本机地址" description={addressHint(lan, config)}>
        {lan && config.addresses.length > 0 ? (
          <div className="space-y-2">
            {config.addresses.map(address => (
              <div key={address} className="flex items-center justify-end gap-2">
                <span className="text-sm break-all">{address}</span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => void navigator.clipboard.writeText(address)}
                >
                  复制
                </Button>
              </div>
            ))}
          </div>
        ) : (
          <span className="text-sm text-muted-foreground">{lan ? '未发现局域网地址' : '仅本机窗口可访问'}</span>
        )}
      </SettingRow>
      <div className="flex justify-end">
        <Button type="button" size="sm" disabled={pending || !dirty} onClick={save}>
          {pending ? '正在重启…' : '保存并重启'}
        </Button>
      </div>
    </SettingsSection>
  )
}

function addressHint(lan: boolean, config: DesktopConfig): string {
  if (!lan) return '仅本机窗口可访问。'
  if (config.addresses.length === 0) return `未发现局域网地址，可在本机用 ${config.localUrl || '本机窗口'} 访问。`
  return '同网段设备打开这些地址。Windows 可能会询问防火墙权限。'
}
