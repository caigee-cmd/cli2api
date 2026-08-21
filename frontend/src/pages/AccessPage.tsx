import { useMemo, useState } from 'react'
import { Button, Card, Input, Label, ListBox, Select, TextArea } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { testChat } from '@/api/overview'
import { absUrl } from '@/lib/url'

export function AccessPage() {
  const { t } = useI18n()
  const { overview } = useOverview()
  const models = overview?.models || []
  const base = absUrl(overview?.access?.openai_base_url || '/v1')
  const [model, setModel] = useState(models[0]?.id || 'qwen3.7-plus')
  const [prompt, setPrompt] = useState('只回复OK')
  const [out, setOut] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)

  const curl = useMemo(
    () => `curl -s ${base}/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"${model || 'qwen3.7-plus'}","messages":[{"role":"user","content":"只回复OK"}]}'`,
    [base, model],
  )

  async function onTest() {
    setBusy(true)
    setOut(t('requesting'))
    try {
      const data = await testChat(model || 'qwen3.7-plus', prompt || '只回复OK')
      setOut(JSON.stringify(data, null, 2))
    } catch (err) {
      setOut(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[.95fr_1.05fr]">
      <Card className="border border-white/10 bg-white/[0.02] p-5 shadow-none">
        <div className="mb-4">
          <div className="text-lg font-semibold">{t('connection')}</div>
          <div className="text-sm text-zinc-400">{t('externalClients')}</div>
        </div>
        <dl className="space-y-3 text-sm">
          <div className="flex items-start justify-between gap-4 border-b border-white/5 pb-2">
            <dt className="text-zinc-500">{t('baseUrl')}</dt>
            <dd className="max-w-[70%] break-all text-right mono text-xs">{base}</dd>
          </div>
          <div className="flex items-start justify-between gap-4 border-b border-white/5 pb-2">
            <dt className="text-zinc-500">{t('protocol')}</dt>
            <dd className="font-medium">OpenAI Chat Completions</dd>
          </div>
        </dl>
        <div className="mt-4 flex flex-wrap gap-2">
          <Button
            variant="secondary"
            onPress={async () => {
              await navigator.clipboard.writeText(base)
              setCopied(true)
              setTimeout(() => setCopied(false), 900)
            }}
          >
            {copied ? t('copied') : t('copyBaseUrl')}
          </Button>
        </div>
        <pre className="mono mt-4 overflow-x-auto rounded-xl border border-white/10 bg-black/30 p-3 text-xs text-zinc-300 whitespace-pre-wrap">
{curl}
        </pre>
      </Card>

      <Card className="border border-white/10 bg-white/[0.02] p-5 shadow-none">
        <div className="mb-4">
          <div className="text-lg font-semibold">{t('quickTest')}</div>
          <div className="text-sm text-zinc-400">{t('waitingRequest')}</div>
        </div>
        <div className="space-y-3">
          <div>
            <div className="mb-1 text-xs text-zinc-500">{t('model')}</div>
            <Select
              selectedKey={model}
              onSelectionChange={(key) => setModel(String(key))}
              aria-label={t('model')}
            >
              <Select.Trigger>
                <Select.Value />
                <Select.Indicator />
              </Select.Trigger>
              <Select.Popover>
                <ListBox>
                  {(models.length ? models : [{ id: 'qwen3.7-plus' }]).map((m) => (
                    <ListBox.Item key={m.id} id={m.id} textValue={m.id}>
                      <Label>{m.id}</Label>
                    </ListBox.Item>
                  ))}
                </ListBox>
              </Select.Popover>
            </Select>
          </div>
          <div>
            <div className="mb-1 text-xs text-zinc-500">{t('prompt')}</div>
            <Input value={prompt} onChange={(e) => setPrompt(e.target.value)} aria-label={t('prompt')} />
          </div>
          <Button isPending={busy} onPress={() => void onTest()}>
            {busy ? t('requesting') : t('send')}
          </Button>
          <TextArea readOnly className="min-h-56 font-mono text-xs" value={out || t('waitingRequest')} />
        </div>
      </Card>
    </div>
  )
}
