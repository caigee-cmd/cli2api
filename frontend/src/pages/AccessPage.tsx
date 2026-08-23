import { useMemo, useState } from 'react'
import { Button, Card, Chip, Input, Label, ListBox, Select, TextArea } from '@heroui/react'
import { Check, Copy, Play, TerminalSquare } from 'lucide-react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { testChat } from '@/api/overview'
import { absUrl } from '@/lib/url'

export function AccessPage() {
  const { t } = useI18n()
  const { overview } = useOverview()
  const models = overview?.models || []
  const accounts = overview?.accounts || []
  const base = absUrl(overview?.access?.openai_base_url || '/v1')
  const [model, setModel] = useState(models[0]?.id || 'qwen3.7-plus')
  const [accountId, setAccountId] = useState('')
  const [prompt, setPrompt] = useState('只回复OK')
  const [output, setOutput] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState<'base' | 'curl' | ''>('')
  const selectedAccount = accountId || ''

  const curl = useMemo(
    () => `curl -s ${base}/chat/completions \\
  -H "Authorization: Bearer $PROXY_API_KEY" \\
  -H "Content-Type: application/json"${selectedAccount ? ` \\
  -H "X-Qoder-Account: ${selectedAccount}"` : ''} \\
  -d '{"model":"${model || 'qwen3.7-plus'}","messages":[{"role":"user","content":"${prompt || '只回复OK'}"}]}'`,
    [base, model, prompt, selectedAccount],
  )

  async function copy(value: string, kind: 'base' | 'curl') {
    await navigator.clipboard.writeText(value)
    setCopied(kind)
    window.setTimeout(() => setCopied(''), 1100)
  }

  async function onTest() {
    setBusy(true)
    setOutput(t('requesting'))
    try {
      const data = await testChat(model || 'qwen3.7-plus', prompt || '只回复OK', selectedAccount || undefined)
      setOutput(JSON.stringify(data, null, 2))
    } catch (error) {
      setOutput(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-6">
      <section className="grid gap-5 border-b border-[var(--app-line)] pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div>
          <p className="mb-2 text-xs font-semibold tracking-[0.14em] text-[var(--accent)] uppercase">OpenAI compatible</p>
          <h2 className="text-3xl font-semibold tracking-[-0.04em] sm:text-4xl">{t('connection')}</h2>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-[var(--app-muted)]">{t('externalClients')}</p>
        </div>
        <Chip variant="soft" color="success">HTTP / SSE</Chip>
      </section>

      <section className="grid gap-5 xl:grid-cols-[minmax(0,.9fr)_minmax(0,1.1fr)]">
        <div className="space-y-5">
          <Card className="app-panel-flat overflow-hidden rounded-xl p-0 shadow-none">
            <div className="flex items-center justify-between border-b border-[var(--app-line)] px-5 py-4">
              <div className="flex items-center gap-3">
                <div className="grid size-9 place-items-center rounded-lg bg-[var(--app-surface-muted)] text-[var(--app-muted)]"><TerminalSquare size={16} /></div>
                <div>
                  <div className="text-sm font-semibold">{t('clientConfig')}</div>
                  <div className="mt-0.5 text-xs text-[var(--app-faint)]">{t('clientConfigHint')}</div>
                </div>
              </div>
              <Button isIconOnly size="sm" variant="ghost" aria-label={t('copyBaseUrl')} onPress={() => void copy(base, 'base')}>
                {copied === 'base' ? <Check size={15} /> : <Copy size={15} />}
              </Button>
            </div>
            <dl className="divide-y divide-[var(--app-line)]">
              <div className="grid gap-2 px-5 py-4 sm:grid-cols-[120px_minmax(0,1fr)]">
                <dt className="text-xs text-[var(--app-faint)]">{t('baseUrl')}</dt>
                <dd className="mono break-all text-xs font-medium sm:text-right">{base}</dd>
              </div>
              <div className="grid gap-2 px-5 py-4 sm:grid-cols-[120px_minmax(0,1fr)]">
                <dt className="text-xs text-[var(--app-faint)]">{t('protocol')}</dt>
                <dd className="text-sm font-medium sm:text-right">OpenAI Chat Completions</dd>
              </div>
              <div className="grid gap-2 px-5 py-4 sm:grid-cols-[120px_minmax(0,1fr)]">
                <dt className="text-xs text-[var(--app-faint)]">{t('authentication')}</dt>
                <dd className="mono text-xs font-medium sm:text-right">Bearer $PROXY_API_KEY</dd>
              </div>
            </dl>
          </Card>

          <Card className="app-panel-flat overflow-hidden rounded-xl p-0 shadow-none">
            <div className="flex items-center justify-between border-b border-[var(--app-line)] px-5 py-3.5">
              <div className="mono text-[10px] font-semibold tracking-[0.08em] text-[var(--app-faint)] uppercase">{t('curlExample')}</div>
              <Button size="sm" variant="ghost" onPress={() => void copy(curl, 'curl')}>
                {copied === 'curl' ? <Check size={14} /> : <Copy size={14} />}
                {copied === 'curl' ? t('copied') : t('copy')}
              </Button>
            </div>
            <pre className="mono min-h-56 overflow-x-auto whitespace-pre-wrap p-5 text-xs leading-6 text-[var(--app-muted)]">{curl}</pre>
          </Card>
        </div>

        <Card className="app-panel overflow-hidden rounded-xl p-0 shadow-none">
          <div className="flex items-center justify-between gap-4 border-b border-[var(--app-line)] px-5 py-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{t('quickTest')}</h3>
              <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('quickTestHint')}</p>
            </div>
            <Button isPending={busy} onPress={() => void onTest()}>
              <Play size={15} />
              {busy ? t('requesting') : t('send')}
            </Button>
          </div>

          <div className="grid gap-5 p-5 lg:grid-cols-2">
            <div className="space-y-4">
              <div>
                <div className="mb-2 text-xs font-medium text-[var(--app-muted)]">{t('account')}</div>
                <Select selectedKey={selectedAccount || 'auto'} onSelectionChange={(key) => setAccountId(String(key) === 'auto' ? '' : String(key))} aria-label={t('account')}>
                  <Select.Trigger><Select.Value /><Select.Indicator /></Select.Trigger>
                  <Select.Popover>
                    <ListBox>
                      <ListBox.Item id="auto" textValue={t('autoAccount')}><Label>{t('autoAccount')}</Label></ListBox.Item>
                      {accounts.map((account) => (
                        <ListBox.Item key={account.id} id={account.id} textValue={account.name || account.id}>
                          <Label>{account.name || account.id}</Label>
                        </ListBox.Item>
                      ))}
                    </ListBox>
                  </Select.Popover>
                </Select>
              </div>
              <div>
                <div className="mb-2 text-xs font-medium text-[var(--app-muted)]">{t('model')}</div>
                <Select selectedKey={model} onSelectionChange={(key) => setModel(String(key))} aria-label={t('model')}>
                  <Select.Trigger><Select.Value /><Select.Indicator /></Select.Trigger>
                  <Select.Popover>
                    <ListBox>
                      {(models.length ? models : [{ id: 'qwen3.7-plus' }]).map((item) => (
                        <ListBox.Item key={item.id} id={item.id} textValue={item.display_name || item.id}>
                          <Label>{item.display_name || item.id} · {item.id}</Label>
                        </ListBox.Item>
                      ))}
                    </ListBox>
                  </Select.Popover>
                </Select>
              </div>
              <div>
                <div className="mb-2 text-xs font-medium text-[var(--app-muted)]">{t('prompt')}</div>
                <Input value={prompt} onChange={(event) => setPrompt(event.target.value)} aria-label={t('prompt')} />
              </div>
            </div>
            <div className="min-w-0">
              <div className="mb-2 flex items-center justify-between text-xs font-medium text-[var(--app-muted)]">
                <span>{t('response')}</span>
                <span className="mono text-[10px] text-[var(--app-faint)]">JSON</span>
              </div>
              <TextArea readOnly className="min-h-72 font-mono text-xs" value={output || t('waitingRequest')} />
            </div>
          </div>
        </Card>
      </section>
    </div>
  )
}
