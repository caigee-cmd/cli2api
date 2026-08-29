import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Checkbox, Chip, Description, Form, Input, Label, Modal } from '@heroui/react'
import { Copy, Key, Plus, TrashSimple, X } from '@phosphor-icons/react'
import { createAPIKey, deleteAPIKey, fetchAPIKeys, updateAPIKey, type APIKeyRecord } from '@/api/keys'
import { BrandMark } from '@/components/BrandMark'
import { ProviderMark } from '@/components/ProviderMark'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { PageAlert } from '@/components/ui/PageAlert'
import { KeysPageSkeleton } from '@/components/ui/PageSkeletons'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { accountProviderFamilyLabel } from '@/lib/provider'

const PROVIDER_IDS = ['qoder', 'workbuddy', 'trae']

function providersLabel(ids: string[], t: (key: string) => string) {
  if (!ids.length) return t('keysAllProviders')
  return ids.map((id) => accountProviderFamilyLabel(id, t)).join(' · ')
}

function formatTime(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export function KeysPage() {
  const { t } = useI18n()
  const { overview } = useOverview()
  const [keys, setKeys] = useState<APIKeyRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [busyId, setBusyId] = useState('')
  const [error, setError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [editKey, setEditKey] = useState<APIKeyRecord | null>(null)
  const [deleteKey, setDeleteKey] = useState<APIKeyRecord | null>(null)
  const [revealed, setRevealed] = useState<APIKeyRecord | null>(null)
  const [copied, setCopied] = useState(false)

  const availableProviders = useMemo(() => {
    const ids = new Set(PROVIDER_IDS)
    for (const account of overview?.accounts || []) {
      if (account.provider) ids.add(account.provider)
    }
    return [...ids]
  }, [overview?.accounts])

  async function load() {
    setLoading(true)
    try {
      const result = await fetchAPIKeys()
      setKeys(result.data || [])
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 0)
    return () => window.clearTimeout(timer)
  }, [])

  async function copySecret(value: string) {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }

  async function onToggle(key: APIKeyRecord, enabled: boolean) {
    setBusyId(key.id)
    setKeys((current) => current.map((item) => item.id === key.id ? { ...item, enabled } : item))
    try {
      const updated = await updateAPIKey(key.id, { enabled })
      setKeys((current) => current.map((item) => item.id === key.id ? { ...item, ...updated, secret: undefined } : item))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      await load()
    } finally {
      setBusyId('')
    }
  }

  async function onDelete() {
    if (!deleteKey) return
    setBusyId(deleteKey.id)
    try {
      await deleteAPIKey(deleteKey.id)
      setKeys((current) => current.filter((item) => item.id !== deleteKey.id))
      setDeleteKey(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusyId('')
    }
  }

  if (loading && !keys.length && !error) return <KeysPageSkeleton />

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-end justify-between gap-4 border-b border-separator pb-4">
        <div>
          <h2 data-gsap-reveal className="text-2xl font-semibold tracking-[-0.035em]">{t('navKeys')}</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-muted">{t('keysLead')}</p>
        </div>
        <Button size="sm" onPress={() => setCreateOpen(true)}>
          <Plus size={14} />{t('keysCreate')}
        </Button>
      </section>

      {error ? <PageAlert title={error} /> : null}

      {!keys.length ? (
        <EmptyPanel
          className="rounded-3xl border border-dashed border-border"
          icon={<BrandMark size={28} />}
          title={t('keysEmpty')}
          hint={t('keysEmptyHint')}
          action={<Button size="sm" onPress={() => setCreateOpen(true)}><Plus size={14} />{t('keysCreate')}</Button>}
        />
      ) : (
        <section className="grid gap-2.5 lg:grid-cols-2 xl:grid-cols-3">
          {keys.map((key) => (
            <Card key={key.id} className="overflow-hidden p-0">
              <Card.Header className="flex-row items-start justify-between gap-3 px-3 pt-3 pb-2.5">
                <div className="min-w-0">
                  <Card.Title className="truncate text-sm tracking-[-0.02em]">{key.name}</Card.Title>
                  <Card.Description className="mono mt-1 truncate">{key.prefix}</Card.Description>
                </div>
                <Chip size="sm" variant="soft" color={key.enabled ? 'success' : 'default'}>
                  {key.enabled ? t('enabled') : t('disabled')}
                </Chip>
              </Card.Header>
              <Card.Content className="space-y-2.5 px-3 pb-2.5">
                <div className="flex flex-wrap items-center gap-1.5">
                  {(key.providers.length ? key.providers : ['all']).map((provider) => (
                    <Chip key={provider} size="sm" variant="soft">
                      <span className="flex items-center gap-1.5">
                        {provider !== 'all' ? <ProviderMark provider={provider} size={12} /> : <Key size={12} />}
                        <span>{provider === 'all' ? t('keysAllProviders') : accountProviderFamilyLabel(provider, t)}</span>
                      </span>
                    </Chip>
                  ))}
                </div>
                <p className="text-[11px] leading-5 text-muted">
                  {key.last_used_at ? t('keysLastUsed', { time: formatTime(key.last_used_at) }) : t('keysNeverUsed')}
                </p>
              </Card.Content>
              <Card.Footer className="gap-1.5 border-t border-separator px-3 py-2">
                <Button size="sm" variant="ghost" onPress={() => setEditKey(key)}>{t('edit')}</Button>
                <Button isIconOnly size="sm" variant="ghost" aria-label={t('delete')} onPress={() => setDeleteKey(key)}>
                  <TrashSimple size={14} />
                </Button>
                <div className="ml-auto">
                  <CompactSwitch
                    isSelected={key.enabled}
                    isDisabled={busyId === key.id}
                    ariaLabel={t('enable')}
                    onChange={(selected) => void onToggle(key, selected)}
                  />
                </div>
              </Card.Footer>
            </Card>
          ))}
        </section>
      )}

      <KeyEditorModal
        isOpen={createOpen}
        title={t('keysCreateTitle')}
        hint={t('keysCreateHint')}
        providers={availableProviders}
        t={t}
        onClose={() => setCreateOpen(false)}
        onSave={async (input) => {
          const created = await createAPIKey(input)
          setKeys((current) => [created, ...current.map((item) => ({ ...item, secret: undefined }))])
          setCreateOpen(false)
          setRevealed(created)
        }}
      />
      <KeyEditorModal
        isOpen={Boolean(editKey)}
        title={t('keysEditTitle')}
        hint={t('keysEditHint')}
        initial={editKey}
        providers={availableProviders}
        t={t}
        onClose={() => setEditKey(null)}
        onSave={async (input) => {
          if (!editKey) return
          const updated = await updateAPIKey(editKey.id, input)
          setKeys((current) => current.map((item) => item.id === editKey.id ? { ...item, ...updated, secret: undefined } : item))
          setEditKey(null)
        }}
      />

      <Modal.Root isOpen={Boolean(revealed?.secret)} onOpenChange={(open: boolean) => { if (!open) setRevealed(null) }}>
        <Modal.Backdrop variant="blur">
          <Modal.Container placement="center" size="lg">
            <Modal.Dialog>
              <Modal.Header className="items-start justify-between gap-4 px-6 pt-6">
                <div>
                  <Modal.Heading className="text-lg font-semibold">{t('keysSecretTitle')}</Modal.Heading>
                  <p className="mt-1.5 text-sm leading-6 text-muted">{t('keysSecretHint')}</p>
                </div>
                <Modal.CloseTrigger aria-label={t('close')} className="grid size-9 place-items-center rounded-lg text-muted hover:bg-surface-secondary"><X size={18} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="px-6 pb-2">
                <Alert status="warning" className="mb-4">
                  <Alert.Indicator />
                  <Alert.Content><Alert.Title>{t('keysSecretOnce')}</Alert.Title></Alert.Content>
                </Alert>
                <code className="mono block break-all rounded-lg border border-separator bg-surface-secondary px-3 py-3 text-sm">{revealed?.secret}</code>
              </Modal.Body>
              <Modal.Footer className="justify-end">
                <Button variant="ghost" onPress={() => setRevealed(null)}>{t('close')}</Button>
                <Button onPress={() => { if (revealed?.secret) void copySecret(revealed.secret) }}>
                  <Copy size={14} />{copied ? t('copied') : t('copy')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal.Root>

      <ConfirmDialog
        isOpen={Boolean(deleteKey)}
        title={t('delete')}
        description={t('keysDeleteConfirm', { name: deleteKey?.name || '' })}
        confirmLabel={t('delete')}
        cancelLabel={t('cancel')}
        closeLabel={t('close')}
        isPending={busyId === deleteKey?.id}
        onClose={() => setDeleteKey(null)}
        onConfirm={() => void onDelete()}
      />
    </div>
  )
}

function KeyEditorModal({
  isOpen,
  title,
  hint,
  initial,
  providers,
  t,
  onClose,
  onSave,
}: {
  isOpen: boolean
  title: string
  hint: string
  initial?: APIKeyRecord | null
  providers: string[]
  t: (key: string, vars?: Record<string, string | number>) => string
  onClose: () => void
  onSave: (input: { name: string; providers: string[]; enabled?: boolean }) => Promise<void>
}) {
  const [name, setName] = useState(initial?.name || '')
  const [selected, setSelected] = useState<string[]>(initial?.providers || [])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!isOpen) return
    setName(initial?.name || '')
    setSelected(initial?.providers || [])
    setError('')
  }, [initial, isOpen])

  function toggle(id: string) {
    setSelected((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id])
  }

  async function submit(event?: { preventDefault(): void }) {
    event?.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setError(t('keysNameRequired'))
      return
    }
    setBusy(true)
    setError('')
    try {
      await onSave({ name: trimmed, providers: selected })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal.Root isOpen={isOpen} onOpenChange={(open: boolean) => { if (!open && !busy) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container placement="center" size="lg" scroll="inside">
          <Modal.Dialog className="sm:min-w-[32rem]">
            <Form onSubmit={submit}>
              <Modal.Header className="items-start justify-between gap-4 px-6 pt-6">
                <div>
                  <Modal.Heading className="text-lg font-semibold tracking-[-0.02em]">{title}</Modal.Heading>
                  <p className="mt-1.5 text-sm leading-6 text-muted">{hint}</p>
                </div>
                <Modal.CloseTrigger isDisabled={busy} aria-label={t('close')} className="grid size-9 place-items-center rounded-lg text-muted hover:bg-surface-secondary"><X size={18} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="space-y-5 px-6 pb-2 pt-1">
                {error ? (
                  <Alert status="danger">
                    <Alert.Indicator />
                    <Alert.Content><Alert.Title>{error}</Alert.Title></Alert.Content>
                  </Alert>
                ) : null}
                <div className="space-y-2">
                  <Label className="text-sm font-medium text-muted">{t('keysName')}</Label>
                  <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('keysNamePh')} />
                </div>
                <div className="space-y-2">
                  <Label className="text-sm font-medium text-muted">{t('keysProviders')}</Label>
                  <Description className="text-xs leading-5 text-muted">{t('keysProvidersHint')}</Description>
                  <div className="grid gap-2 sm:grid-cols-2">
                    {providers.map((id) => {
                      const checked = selected.includes(id)
                      return (
                        <Checkbox key={id} isSelected={checked} onChange={() => toggle(id)} className="rounded-lg border border-separator px-3 py-2.5">
                          <Checkbox.Content className="flex items-center gap-2.5">
                            <Checkbox.Control>
                              <Checkbox.Indicator />
                            </Checkbox.Control>
                            <ProviderMark provider={id} size={16} />
                            <span className="text-sm font-medium">{accountProviderFamilyLabel(id, t)}</span>
                          </Checkbox.Content>
                        </Checkbox>
                      )
                    })}
                  </div>
                  <p className="text-xs text-muted">{providersLabel(selected, t)}</p>
                </div>
              </Modal.Body>
              <Modal.Footer className="justify-end">
                <Button variant="ghost" isDisabled={busy} onPress={onClose}>{t('cancel')}</Button>
                <Button type="submit" isPending={busy}>{initial ? t('save') : t('create')}</Button>
              </Modal.Footer>
            </Form>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
