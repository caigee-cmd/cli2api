import { copyText } from '@/lib/clipboard'
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Checkbox, Chip, Description, Form, Input, Label, Modal } from '@heroui/react'
import { Copy, Key, Plus, TrashSimple, X } from '@phosphor-icons/react'
import { createAPIKey, deleteAPIKey, fetchAPIKeys, updateAPIKey, type APIKeyRecord } from '@/api/keys'
import { fetchProviders, type ProviderDescriptor } from '@/api/overview'
import { BrandMark } from '@/components/BrandMark'
import { ProviderMark } from '@/components/ProviderMark'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { FormRow } from '@/components/ui/FormRow'
import { PageAlert } from '@/components/ui/PageAlert'
import { KeysPageSkeleton, SkeletonBlock } from '@/components/ui/PageSkeletons'
import { useI18n } from '@/hooks/useI18n'
import { accountProviderFamilyLabel, accountProviderLabel } from '@/lib/provider'

// One selectable option per provider region, generated from the backend
// provider descriptors. The value is the stored grant entry format
// ("workbuddy:cn"); bare entries ("workbuddy") only come from legacy keys.
type ProviderRegionOption = {
  value: string
  provider: string
  region: string
}

function providerRegionOptions(descriptors: ProviderDescriptor[]): ProviderRegionOption[] {
  const options: ProviderRegionOption[] = []
  for (const descriptor of descriptors) {
    for (const region of descriptor.regions || []) {
      if (!region?.id) continue
      options.push({ value: `${descriptor.id}:${region.id}`, provider: descriptor.id, region: region.id })
    }
  }
  return options
}

// Grant entries stored on a key: either a bare family ("workbuddy", legacy)
// or "family:region". Rendering splits them back into provider + region so
// the region-aware labels apply to both.
function grantLabel(entry: string, t: (key: string) => string) {
  const separator = entry.indexOf(':')
  const provider = separator === -1 ? entry : entry.slice(0, separator)
  const region = separator === -1 ? '' : entry.slice(separator + 1)
  if (!region) return accountProviderFamilyLabel(provider, t)
  return accountProviderLabel(provider, region, t)
}

function grantProvider(entry: string) {
  const separator = entry.indexOf(':')
  return separator === -1 ? entry : entry.slice(0, separator)
}

// Legacy keys store bare family grants ("workbuddy" = every region). The
// editor renders them as "all regions" of that family: every region checkbox
// is pre-checked and the key row shows an all-regions badge. Unticking any
// region drops the bare grant, so an explicit edit can only ever submit
// region-scoped entries — a bare grant can never silently keep a key broader
// than what the user sees checked.
function expandLegacyGrants(entries: string[], options: ProviderRegionOption[]): string[] {
  const expanded: string[] = []
  const bareFamilies = new Set(
    entries.filter((entry) => !entry.includes(':')).map((entry) => entry.toLowerCase()),
  )
  for (const entry of entries) {
    if (!entry.includes(':')) continue
    expanded.push(entry)
  }
  for (const option of options) {
    if (bareFamilies.has(option.provider) && !expanded.includes(option.value)) {
      expanded.push(option.value)
    }
  }
  return expanded
}

function providersLabel(entries: string[], t: (key: string) => string) {
  if (!entries.length) return t('keysAllProviders')
  return entries.map((entry) => grantLabel(entry, t)).join(' · ')
}

function formatTime(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export function KeysPage() {
  const { t } = useI18n()
  const [options, setOptions] = useState<ProviderRegionOption[]>([])
  // The key editor cannot safely submit until the provider descriptor options
  // are loaded: an empty region list would otherwise submit providers=[] —
  // which the backend reads as "allow everything". Both create and edit stay
  // disabled until optionsReady, and a failed load surfaces as an error
  // instead of being swallowed.
  const [optionsReady, setOptionsReady] = useState(false)
  const [optionsError, setOptionsError] = useState('')
  const [keys, setKeys] = useState<APIKeyRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [busyId, setBusyId] = useState('')
  const [error, setError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [editKey, setEditKey] = useState<APIKeyRecord | null>(null)
  const [deleteKey, setDeleteKey] = useState<APIKeyRecord | null>(null)
  const [revealed, setRevealed] = useState<APIKeyRecord | null>(null)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    void fetchProviders().then((result) => {
      const nextOptions = providerRegionOptions(result.data || [])
      setOptions(nextOptions)
      // An empty region list is not a usable editor state: keep the editor
      // disabled so a restricted key can never be saved as "allow all".
      setOptionsReady(nextOptions.length > 0)
      setOptionsError('')
    }).catch((err) => {
      setOptionsError(err instanceof Error ? err.message : String(err))
    })
  }, [])

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
    await copyText(value)
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

  if (loading && !keys.length) return <KeysPageSkeleton />

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-end justify-between gap-4 border-b border-separator pb-4">
        <div>
          <h2 data-gsap-reveal className="text-2xl font-semibold tracking-[-0.035em]">{t('navKeys')}</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-muted">{t('keysLead')}</p>
        </div>
        <Button size="sm" isDisabled={!optionsReady} onPress={() => setCreateOpen(true)}>
          <Plus size={14} />{t('keysCreate')}
        </Button>
      </section>

      {error ? <PageAlert title={error} /> : null}
      {optionsError ? <PageAlert title={t('keysProvidersLoadFailed', { message: optionsError })} /> : null}

      {loading ? (
        <section className="grid gap-2.5 lg:grid-cols-2 xl:grid-cols-3" aria-busy>
          {Array.from({ length: Math.max(3, keys.length) }, (_, index) => (
            <div key={index} className="overflow-hidden rounded-3xl border border-border bg-surface p-3">
              <SkeletonBlock className="h-4 w-28" />
              <SkeletonBlock className="mt-2 h-3 w-40" />
              <SkeletonBlock className="mt-4 h-6 w-24" />
              <SkeletonBlock className="mt-6 h-8 w-full" />
            </div>
          ))}
        </section>
      ) : !keys.length ? (
        <EmptyPanel
          className="rounded-3xl border border-dashed border-border"
          icon={<BrandMark size={28} />}
          title={t('keysEmpty')}
          hint={t('keysEmptyHint')}
          action={<Button size="sm" isDisabled={!optionsReady} onPress={() => setCreateOpen(true)}><Plus size={14} />{t('keysCreate')}</Button>}
        />
      ) : (
        <section className="grid gap-2.5 lg:grid-cols-2 xl:grid-cols-3">
          {keys.map((key) => (
            <Card key={key.id} className="overflow-hidden p-0">
              <Card.Header className="flex-row items-start justify-between gap-3 px-3 pt-3 pb-2.5">
                <div className="min-w-0">
                  <Card.Title className="truncate text-sm tracking-[-0.015em]">{key.name}</Card.Title>
                  <Card.Description className="mono mt-1 truncate">{key.prefix}</Card.Description>
                </div>
                <Chip size="sm" variant="soft" color={key.enabled ? 'success' : 'default'}>
                  {key.enabled ? t('enabled') : t('disabled')}
                </Chip>
              </Card.Header>
              <Card.Content className="space-y-2.5 px-3 pb-2.5">
                <div className="flex flex-wrap items-center gap-1.5">
                  {(key.providers.length ? key.providers : ['all']).map((entry) => (
                    <Chip key={entry} size="sm" variant="soft">
                      <span className="flex items-center gap-1.5">
                        {entry !== 'all' ? <ProviderMark provider={grantProvider(entry)} size={12} /> : <Key size={12} />}
                        <span>{entry === 'all' ? t('keysAllProviders') : grantLabel(entry, t)}</span>
                      </span>
                    </Chip>
                  ))}
                </div>
                <p className="text-[11px] leading-5 text-muted">
                  {key.last_used_at ? t('keysLastUsed', { time: formatTime(key.last_used_at) }) : t('keysNeverUsed')}
                </p>
              </Card.Content>
              <Card.Footer className="gap-1.5 border-t border-separator px-3 py-2">
                <Button size="sm" variant="ghost" isDisabled={!optionsReady} onPress={() => setEditKey(key)}>{t('edit')}</Button>
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
        options={options}
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
        options={options}
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
  options,
  t,
  onClose,
  onSave,
}: {
  isOpen: boolean
  title: string
  hint: string
  initial?: APIKeyRecord | null
  options: ProviderRegionOption[]
  t: (key: string, vars?: Record<string, string | number>) => string
  onClose: () => void
  onSave: (input: { name: string; providers: string[]; enabled?: boolean }) => Promise<void>
}) {
  const [name, setName] = useState(initial?.name || '')
  // Region-scoped checkbox values. Legacy bare grants ("workbuddy") are
  // expanded to all of that family's region checkboxes on open and tracked
  // in allRegionsFamilies (badge display). While providersUntouched stays
  // true the original grant list — including bare entries — is submitted
  // verbatim, so opening a legacy key and saving (even after a rename) keeps
  // `["workbuddy"]` exactly: a future new region of the family stays allowed.
  // The first region toggle flips providersUntouched; from then on only
  // explicit region entries are ever submitted.
  const [selected, setSelected] = useState<string[]>([])
  const [allRegionsFamilies, setAllRegionsFamilies] = useState<Set<string>>(new Set())
  const [providersUntouched, setProvidersUntouched] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!isOpen) return
    setName(initial?.name || '')
    setSelected(expandLegacyGrants(initial?.providers || [], options))
    const bare = new Set(
      (initial?.providers || []).filter((entry) => !entry.includes(':')).map((entry) => entry.toLowerCase()),
    )
    setAllRegionsFamilies(bare)
    setProvidersUntouched(true)
    setError('')
  }, [initial, isOpen, options])

  function toggle(id: string) {
    const option = options.find((item) => item.value === id)
    setProvidersUntouched(false)
    setSelected((current) => {
      if (current.includes(id)) {
        // Unticking a region of a family that was "all regions" drops the
        // all-regions state — the family becomes explicitly region-scoped.
        if (option) setAllRegionsFamilies((families) => {
          const next = new Set(families)
          next.delete(option.provider)
          return next
        })
        return current.filter((item) => item !== id)
      }
      return [...current, id]
    })
  }

  async function submit(event?: { preventDefault(): void }) {
    event?.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setError(t('keysNameRequired'))
      return
    }
    // Fail closed: with the provider options not loaded the region list is
    // unknown, and submitting it would turn a restricted key into
    // "allow everything". Block the save instead.
    if (!options.length) {
      setError(t('keysProvidersNotLoaded'))
      return
    }
    setBusy(true)
    setError('')
    try {
      // Untouched provider selection keeps the stored grants verbatim
      // (legacy bare entries included); any toggle converts to the explicit
      // region list the user sees checked.
      const providers = providersUntouched ? (initial?.providers || []) : selected
      await onSave({ name: trimmed, providers })
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
                  <Modal.Heading className="text-lg font-semibold tracking-[-0.015em]">{title}</Modal.Heading>
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
                <FormRow label={t('keysName')}>
                  <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('keysNamePh')} aria-label={t('keysName')} />
                </FormRow>
                <div className="space-y-2">
                  <Label className="text-sm font-medium text-muted">{t('keysProviders')}</Label>
                  <Description className="text-xs leading-5 text-muted">{t('keysProvidersHint')}</Description>
                  {!options.length ? (
                    <p className="rounded-lg border border-separator bg-surface-secondary px-3 py-2.5 text-xs leading-5 text-muted">{t('keysProvidersNotLoaded')}</p>
                  ) : (
                    <div className="grid gap-2 sm:grid-cols-2">
                      {options.map((option) => {
                        const checked = selected.includes(option.value)
                        const allRegions = allRegionsFamilies.has(option.provider)
                        return (
                          <Checkbox key={option.value} isSelected={checked} onChange={() => toggle(option.value)} className="rounded-xl border border-separator px-3 py-2.5 data-selected:border-accent data-selected:bg-accent-soft">
                            <Checkbox.Content className="flex items-center gap-2.5">
                              <Checkbox.Control>
                                <Checkbox.Indicator />
                              </Checkbox.Control>
                              <ProviderMark provider={option.provider} size={16} />
                              <span className="text-sm font-medium">{accountProviderLabel(option.provider, option.region, t)}</span>
                              {allRegions ? (
                                <span className="rounded-full bg-accent-soft px-1.5 py-0.5 text-[10px] font-medium text-accent">{t('keysAllRegions')}</span>
                              ) : null}
                            </Checkbox.Content>
                          </Checkbox>
                        )
                      })}
                    </div>
                  )}
                  <p className="text-xs text-muted">{providersLabel(selected, t)}</p>
                </div>
              </Modal.Body>
              <Modal.Footer className="justify-end">
                <Button variant="ghost" isDisabled={busy} onPress={onClose}>{t('cancel')}</Button>
                <Button type="submit" isPending={busy} isDisabled={!options.length}>{initial ? t('save') : t('create')}</Button>
              </Modal.Footer>
            </Form>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
