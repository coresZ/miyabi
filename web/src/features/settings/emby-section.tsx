import { useState } from 'react'
import { CheckCircle2Icon, LoaderCircleIcon, TvMinimalIcon } from 'lucide-react'
import { toast } from 'sonner'

import { type EmbyConfig, useEmbyConfig, useTestEmbyConfig, useUpdateEmbyConfig } from '@/api/emby'
import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { SettingRow, SettingsSection } from './shared'

export function EmbySection() {
  const emby = useEmbyConfig()
  const updateConfig = useUpdateEmbyConfig()
  const testConfig = useTestEmbyConfig()

  const config = emby.data
  const [form, setForm] = useState<EmbyConfig | null>(null)
  const [testResult, setTestResult] = useState<{ name: string; version: string } | null>(null)

  const current: EmbyConfig = form ??
    config ?? {
      enabled: false,
      server_url: '',
      api_key: '',
      media_path: ''
    }

  const isDirty =
    Boolean(config) &&
    form !== null &&
    (form.enabled !== config!.enabled ||
      form.server_url !== config!.server_url ||
      form.api_key !== config!.api_key ||
      form.media_path !== config!.media_path)

  const disabled = emby.isLoading || emby.isError || updateConfig.isPending

  function updateField<K extends keyof EmbyConfig>(key: K, value: EmbyConfig[K]) {
    setForm(prev => ({
      ...(prev ??
        config ?? {
          enabled: false,
          server_url: '',
          api_key: '',
          media_path: ''
        }),
      [key]: value
    }))
  }

  function handleTest() {
    const trimmedUrl = current.server_url.trim()
    const trimmedKey = current.api_key.trim()

    if (!trimmedUrl) {
      toast.error('请填写 Emby 服务器地址')
      return
    }
    if (!trimmedKey) {
      toast.error('请填写 Emby API Key')
      return
    }

    setTestResult(null)
    testConfig.mutate(
      { server_url: trimmedUrl, api_key: trimmedKey },
      {
        onSuccess: data => {
          setTestResult({ name: data.server_name, version: data.version })
          toast.success(`连接成功：${data.server_name} (v${data.version})`)
        },
        onError: error => {
          toast.error(error instanceof Error ? error.message : '连接 Emby 服务器失败')
        }
      }
    )
  }

  function handleSave() {
    const trimmedUrl = current.server_url.trim()
    const trimmedKey = current.api_key.trim()

    if (current.enabled) {
      if (!trimmedUrl) {
        toast.error('启用 Emby 集成时必须填写服务器地址')
        return
      }
      if (!trimmedKey) {
        toast.error('启用 Emby 集成时必须填写 API Key')
        return
      }
    }

    const payload: EmbyConfig = {
      enabled: current.enabled,
      server_url: trimmedUrl,
      api_key: trimmedKey,
      media_path: current.media_path.trim()
    }

    updateConfig.mutate(payload, {
      onSuccess: () => {
        setForm(null)
        toast.success('Emby 设置已保存')
      },
      onError: error => {
        toast.error(error instanceof Error ? error.message : '保存 Emby 设置失败')
      }
    })
  }

  return (
    <SettingsSection icon={<TvMinimalIcon className="size-4" />} title="Emby">
      <SettingRow
        title="启用 Emby"
        description="媒体扫描与刮削完成后，主动通知 Emby 增量刷新，实现零风控实时影视墙"
        inline
      >
        <Switch
          checked={current.enabled}
          disabled={disabled}
          onCheckedChange={checked => updateField('enabled', checked)}
        />
      </SettingRow>

      <SettingRow
        title="服务器地址"
        description="Emby 服务的访问地址，例如 http://192.168.1.100:8096"
      >
        <Input
          value={current.server_url}
          placeholder="http://192.168.1.100:8096"
          className="w-full sm:w-80"
          disabled={disabled}
          onChange={e => updateField('server_url', e.target.value)}
        />
      </SettingRow>

      <SettingRow title="API Key" description="在 Emby 管理后台「高级」→「API 密钥」中生成">
        <Input
          type="password"
          value={current.api_key}
          placeholder="填入 Emby API Key"
          className="w-full sm:w-80"
          disabled={disabled}
          onChange={e => updateField('api_key', e.target.value)}
        />
      </SettingRow>

      <SettingRow
        title="Emby 媒体库路径"
        description="Emby 容器内挂载的对应目录。留空则默认使用本地导出路径"
      >
        <Input
          value={current.media_path}
          placeholder="/media"
          className="w-full sm:w-80"
          disabled={disabled}
          onChange={e => updateField('media_path', e.target.value)}
        />
      </SettingRow>

      <SettingRow
        title="连通性与保存"
        description={
          testResult ? (
            <span className="inline-flex items-center gap-1.5 text-xs text-success">
              <CheckCircle2Icon className="size-3.5" />
              已连接：{testResult.name} ({testResult.version})
            </span>
          ) : (
            '测试当前填写的配置是否能正常连接 Emby'
          )
        }
      >
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled || testConfig.isPending}
            onClick={handleTest}
          >
            {testConfig.isPending ? (
              <LoaderCircleIcon className="mr-1.5 size-3.5 animate-spin" />
            ) : null}
            测试连接
          </Button>
          <Button type="button" size="sm" disabled={disabled || !isDirty} onClick={handleSave}>
            {updateConfig.isPending ? (
              <LoaderCircleIcon className="mr-1.5 size-3.5 animate-spin" />
            ) : null}
            保存设置
          </Button>
        </div>
      </SettingRow>

      {emby.isError ? <InlineError>后端服务暂不可用，无法读取 Emby 配置。</InlineError> : null}
    </SettingsSection>
  )
}
