import { useMemo, useState } from 'react'
import { Button, Card, Chip } from '@heroui/react'
import { ArrowSquareOut, BracketsCurly, Check, Copy, Heartbeat, ListBullets, PaperPlaneTilt } from '@phosphor-icons/react'
import type { Overview } from '@/api/types'
import { useI18n } from '@/hooks/useI18n'
import { absUrl } from '@/lib/url'

type EndpointListProps = {
  access?: Overview['access']
}

export function EndpointList({ access }: EndpointListProps) {
  const { t } = useI18n()
  const [copiedEndpoint, setCopiedEndpoint] = useState('')
  const base = absUrl(access?.openai_base_url || '/v1')
  const endpoints = useMemo(() => [
    { name: t('endpointOpenAI'), url: base, method: 'BASE', hint: t('endpointBaseHint'), icon: <BracketsCurly size={17} /> },
    { name: t('endpointChat'), url: absUrl(access?.chat_completions || `${base}/chat/completions`), method: 'POST', hint: t('endpointChatHint'), icon: <PaperPlaneTilt size={17} /> },
    { name: t('endpointMessages'), url: absUrl(access?.messages || `${base}/messages`), method: 'POST', hint: t('endpointMessagesHint'), icon: <PaperPlaneTilt size={17} /> },
    { name: t('endpointResponses'), url: absUrl(access?.responses || `${base}/responses`), method: 'POST', hint: t('endpointResponsesHint'), icon: <PaperPlaneTilt size={17} /> },
    { name: t('endpointModels'), url: absUrl(access?.models || `${base}/models`), method: 'GET', hint: t('endpointModelsHint'), icon: <ListBullets size={17} /> },
    { name: t('endpointHealth'), url: absUrl(access?.health || '/health'), method: 'GET', hint: t('endpointHealthHint'), icon: <Heartbeat size={17} /> },
  ], [access, base, t])

  return (
    <Card data-gsap-reveal className="overflow-hidden border-border p-0 shadow-none">
      <div className="flex items-start justify-between gap-4 border-b border-separator px-5 py-4 sm:px-6">
        <div>
          <div className="flex items-center gap-2">
            <h3 className="font-semibold tracking-[-0.015em]">{t('endpoints')}</h3>
            <Chip size="sm" variant="soft">{t('endpointCount', { count: endpoints.length - 1 })}</Chip>
          </div>
          <p className="mt-1 text-xs leading-5 text-muted">{t('routesHint')}</p>
        </div>
      </div>
      <div className="grid gap-2.5 p-3 sm:grid-cols-2 sm:p-4 lg:grid-cols-3">
        {endpoints.map((item) => (
          <div key={item.name} className="group min-w-0 rounded-lg bg-surface-secondary/45 p-3 transition-colors hover:bg-surface-secondary/65">
            <div className="flex items-start justify-between gap-2.5">
              <div className="flex min-w-0 items-center gap-2.5">
                <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-surface text-muted">{item.icon}</span>
                <div className="min-w-0">
                  <div className="truncate text-sm font-semibold">{item.name}</div>
                  <code className="mono mt-0.5 block truncate text-[11px] text-muted">{item.url}</code>
                </div>
              </div>
              <Chip size="sm" variant="soft">{item.method}</Chip>
            </div>
            <p className="mt-2 min-h-0 text-xs leading-5 text-muted">{item.hint}</p>
            <div className="mt-2 flex items-center justify-between gap-2 border-t border-separator pt-2">
              <span className="text-[10px] font-medium text-muted">{item.method === 'BASE' ? t('endpointBaseLabel') : t('endpointAuthLabel')}</span>
              <div className="flex gap-1">
                <Button isIconOnly size="sm" variant="ghost" aria-label={t('copy')} onPress={() => { void navigator.clipboard.writeText(item.url); setCopiedEndpoint(item.name); window.setTimeout(() => setCopiedEndpoint(''), 1100) }}>
                  {copiedEndpoint === item.name ? <Check size={14} className="text-success" /> : <Copy size={14} />}
                </Button>
                <Button isIconOnly size="sm" variant="ghost" aria-label={t('open')} onPress={() => window.open(item.url, '_blank', 'noopener,noreferrer')}>
                  <ArrowSquareOut size={14} />
                </Button>
              </div>
            </div>
          </div>
        ))}
      </div>
    </Card>
  )
}
