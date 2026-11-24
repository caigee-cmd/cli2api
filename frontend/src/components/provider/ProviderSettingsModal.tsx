import { useEffect, useState } from 'react'
import { Alert, Button, Chip, Form, Input, Modal, NumberField } from '@heroui/react'
import { X } from '@phosphor-icons/react'
import { ProviderMark } from '@/components/ProviderMark'
import { FormRow } from '@/components/ui/FormRow'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { ModelPicker } from '@/components/account/ModelPicker'
import { useCollapse } from '@/hooks/useCollapse'
import { fetchProviderCatalog, updateProviderPolicy, type ProviderPolicy } from '@/api/providerSettings'

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  provider: string
  region: string
  label: string
  policy: ProviderPolicy
  checkinSupported: boolean
  checkinTimezone?: string
  t: Translate
  onClose: () => void
  onSaved: (policy: ProviderPolicy) => void
}

function defaultDropSystemPrompt(provider: string) {
  const value = provider.toLowerCase()
  return !['trae', 'kimi', 'codearts', 'minimaxcode'].includes(value)
}

function supportsDropSwitch(provider: string) {
  const value = provider.toLowerCase()
  return value === 'workbuddy' || value === 'trae'
}

function autoCheckinFallback(provider: string) {
  return provider.toLowerCase() === 'qoder' ? '11:00' : '01:00'
}

export function ProviderSettingsModal({
  provider,
  region,
  label,
  policy,
  checkinSupported,
  checkinTimezone,
  t,
  onClose,
  onSaved,
}: Props) {
  const [creditFloorMode, setCreditFloorMode] = useState<'inherit' | 'custom'>(
    policy.credit_floor == null ? 'inherit' : 'custom',
  )
  const [creditFloor, setCreditFloor] = useState<number>(policy.credit_floor ?? 0)
  const [percentFloorMode, setPercentFloorMode] = useState<'inherit' | 'custom'>(
    policy.percent_floor == null ? 'inherit' : 'custom',
  )
  const [percentFloor, setPercentFloor] = useState<number>(policy.percent_floor ?? 0)
  const [proxyUrl, setProxyUrl] = useState(policy.proxy_url || '')
  const [dropSystemPrompt, setDropSystemPrompt] = useState(
    policy.drop_system_prompt ?? defaultDropSystemPrompt(provider),
  )
  const [aliases, setAliases] = useState<Array<{ alias: string; model: string }>>(
    Object.entries(policy.model_aliases || {}).map(([alias, model]) => ({ alias, model })),
  )
  const [aliasDraft, setAliasDraft] = useState({ alias: '', model: '' })
  const [restrictModels, setRestrictModels] = useState(
    Boolean(policy.models_restricted) || Boolean((policy.enabled_models || []).length),
  )
  const [enabledModels, setEnabledModels] = useState<string[]>(policy.enabled_models || [])
  const { mounted: pickerMounted, outerRef: pickerOuterRef, innerRef: pickerInnerRef } = useCollapse(restrictModels)
  const [catalog, setCatalog] = useState<string[] | null>(null)
  const [catalogError, setCatalogError] = useState('')
  const [checkinTime, setCheckinTime] = useState(policy.checkin_time || (provider.toLowerCase() === 'qoder' ? '11:00' : '01:00'))
  const [checkinAuto, setCheckinAuto] = useState(policy.checkin_auto !== false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    void fetchProviderCatalog(provider, region)
      .then((result) => {
        if (cancelled) return
        setCatalog((result.data || []).map((entry) => entry.id))
        setCatalogError('')
      })
      .catch((err) => {
        if (cancelled) return
        setCatalog([])
        setCatalogError(err instanceof Error ? err.message : String(err))
      })
    return () => { cancelled = true }
  }, [provider, region])

  async function submit(event?: { preventDefault(): void }) {
    event?.preventDefault()
    if (creditFloorMode === 'custom' && (!Number.isFinite(creditFloor) || creditFloor < 0)) {
      setError(t('creditFloorInvalid'))
      return
    }
    if (percentFloorMode === 'custom' && (!Number.isFinite(percentFloor) || percentFloor < 0 || percentFloor > 100)) {
      setError(t('percentFloorInvalid'))
      return
    }
    if (checkinSupported && !checkinAuto && !/^([01]\d|2[0-3]):[0-5]\d$/.test(checkinTime)) {
      setError(t('checkinTimeInvalid'))
      return
    }
    if (restrictModels && enabledModels.length === 0) {
      setError(t('providerModelsEmpty'))
      return
    }
    setError('')
    setSaving(true)
    try {
      const payload: ProviderPolicy = {
        provider,
        region,
        credit_floor: creditFloorMode === 'custom' ? creditFloor : null,
        percent_floor: percentFloorMode === 'custom' ? percentFloor : null,
        proxy_url: proxyUrl.trim(),
        drop_system_prompt: dropSystemPrompt,
        model_aliases: Object.fromEntries(
          aliases
            .filter((item) => item.alias.trim() && item.model.trim())
            .map((item) => [item.alias.trim(), item.model.trim()]),
        ),
        enabled_models: restrictModels ? enabledModels : [],
        models_restricted: restrictModels,
        checkin_time: checkinSupported ? checkinTime : '',
        checkin_auto: checkinSupported ? checkinAuto : false,
      }
      const result = await updateProviderPolicy(payload)
      onSaved(result.data || payload)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal.Root isOpen onOpenChange={(next: boolean) => { if (!next && !saving) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container placement="center" size="lg" scroll="inside">
          <Modal.Dialog className="w-full sm:w-[42rem] sm:max-w-[calc(100vw-2rem)]">
            <Modal.Header className="items-start justify-between gap-4 px-6 pt-6">
              <div className="min-w-0">
                <Modal.Heading className="text-section font-semibold">{t('providerSettingsTitle')}</Modal.Heading>
                <p className="mt-1.5 text-body font-normal leading-6 text-muted">{t('providerSettingsHint')}</p>
                <div className="mt-3 flex flex-wrap items-center gap-2">
                  <Chip size="sm" variant="soft">
                    <span className="flex items-center gap-1.5">
                      <ProviderMark provider={provider} size={14} />
                      <span>{label}</span>
                    </span>
                  </Chip>
                  <span className="mono text-caption text-muted">{region || 'any'}</span>
                </div>
              </div>
              <Modal.CloseTrigger isDisabled={saving} aria-label={t('close')} className="grid size-9 shrink-0 place-items-center rounded-lg text-muted hover:bg-surface-secondary">
                <X size={18} />
              </Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="px-6 pb-2 pt-1">
              {error ? (
                <Alert status="danger" className="mb-4">
                  <Alert.Indicator />
                  <Alert.Content>
                    <Alert.Title>{error}</Alert.Title>
                  </Alert.Content>
                </Alert>
              ) : null}
              {catalogError ? (
                <Alert status="warning" className="mb-4">
                  <Alert.Indicator />
                  <Alert.Content>
                    <Alert.Title>{catalogError}</Alert.Title>
                  </Alert.Content>
                </Alert>
              ) : null}
              <Form className="space-y-4" onSubmit={(event) => void submit(event)}>
                <FormRow label={t('creditFloor')} hint={t('creditFloorHint')} align="start">
                  <div className="space-y-2">
                    <CompactSwitch
                      isSelected={creditFloorMode === 'custom'}
                      isDisabled={saving}
                      ariaLabel={t('creditFloorCustom')}
                      label={t('creditFloorCustom')}
                      onChange={(selected) => setCreditFloorMode(selected ? 'custom' : 'inherit')}
                    />
                    {creditFloorMode === 'custom' ? (
                      <NumberField
                        value={creditFloor}
                        onChange={(value) => setCreditFloor(value ?? 0)}
                        minValue={0}
                        step={1}
                        isDisabled={saving}
                        isRequired
                      >
                        <NumberField.Group>
                          <NumberField.DecrementButton />
                          <NumberField.Input aria-label={t('creditFloor')} />
                          <NumberField.IncrementButton />
                        </NumberField.Group>
                      </NumberField>
                    ) : (
                      <p className="text-caption text-muted">{t('creditFloorInheritHint')}</p>
                    )}
                  </div>
                </FormRow>
                <FormRow label={t('percentFloor')} hint={t('percentFloorHint')} align="start">
                  <div className="space-y-2">
                    <CompactSwitch
                      isSelected={percentFloorMode === 'custom'}
                      isDisabled={saving}
                      ariaLabel={t('percentFloorCustom')}
                      label={t('percentFloorCustom')}
                      onChange={(selected) => setPercentFloorMode(selected ? 'custom' : 'inherit')}
                    />
                    {percentFloorMode === 'custom' ? (
                      <NumberField
                        value={percentFloor}
                        onChange={(value) => setPercentFloor(value ?? 0)}
                        minValue={0}
                        maxValue={100}
                        step={1}
                        isDisabled={saving}
                        isRequired
                      >
                        <NumberField.Group>
                          <NumberField.DecrementButton />
                          <NumberField.Input aria-label={t('percentFloor')} />
                          <NumberField.IncrementButton />
                        </NumberField.Group>
                      </NumberField>
                    ) : (
                      <p className="text-caption text-muted">{t('percentFloorInheritHint')}</p>
                    )}
                  </div>
                </FormRow>
                <FormRow label={t('proxyUrl')} hint={t('proxyUrlHint')}>
                  <Input
                    value={proxyUrl}
                    onChange={(event) => setProxyUrl(event.target.value)}
                    placeholder={t('proxyUrlPlaceholder')}
                    aria-label={t('proxyUrl')}
                    disabled={saving}
                  />
                </FormRow>
                {supportsDropSwitch(provider) ? (
                  <FormRow label={t('dropSystemPrompt')} hint={t(provider === 'trae' ? 'dropSystemPromptTraeHint' : 'dropSystemPromptHint')}>
                    <CompactSwitch
                      isSelected={dropSystemPrompt}
                      isDisabled={saving}
                      ariaLabel={t('dropSystemPrompt')}
                      onChange={setDropSystemPrompt}
                    />
                  </FormRow>
                ) : null}
                <FormRow align="start" label={t('accountModelAliases')} hint={t('accountModelAliasesHint')}>
                  <div className="space-y-2">
                    {aliases.map((item) => (
                      <div key={item.alias} className="flex items-center justify-between gap-2">
                        <span className="mono min-w-0 truncate text-body">{item.alias} → {item.model}</span>
                        <Button size="sm" variant="ghost" isDisabled={saving} onPress={() => setAliases((current) => current.filter((row) => row.alias !== item.alias))}>{t('modelAliasRemove')}</Button>
                      </div>
                    ))}
                    <div className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
                      <Input aria-label={t('modelAliasName')} value={aliasDraft.alias} onChange={(event) => setAliasDraft((current) => ({ ...current, alias: event.target.value }))} placeholder={t('modelAliasPlaceholder')} disabled={saving} />
                      <Input aria-label={t('modelAliasTarget')} value={aliasDraft.model} onChange={(event) => setAliasDraft((current) => ({ ...current, model: event.target.value }))} placeholder={t('accountModelAliasTargetPlaceholder')} disabled={saving} />
                      <Button size="sm" variant="secondary" isDisabled={saving || !aliasDraft.alias.trim() || !aliasDraft.model.trim()} onPress={() => {
                        setAliases((current) => [...current.filter((row) => row.alias !== aliasDraft.alias.trim()), { alias: aliasDraft.alias.trim(), model: aliasDraft.model.trim() }])
                        setAliasDraft({ alias: '', model: '' })
                      }}>{t('modelAliasAdd')}</Button>
                    </div>
                  </div>
                </FormRow>
                <section className="space-y-3 pt-1">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="text-body font-medium text-foreground">{t('enabledModels')}</div>
                      <p className="mt-0.5 text-caption leading-5 text-muted">{t('enabledModelsHint')}</p>
                    </div>
                    <CompactSwitch isSelected={restrictModels} isDisabled={saving} ariaLabel={t('restrictModels')} label={restrictModels ? t('restrictModels') : t('allModels')} onChange={setRestrictModels} />
                  </div>
                  {pickerMounted ? (
                    <div ref={pickerOuterRef}>
                      <div ref={pickerInnerRef}>
                        <ModelPicker
                          catalog={catalog}
                          selected={enabledModels}
                          onChange={setEnabledModels}
                          isDisabled={saving}
                          t={t}
                        />
                      </div>
                    </div>
                  ) : null}
                </section>
                {checkinSupported ? (
                  <FormRow label={t('autoCheckinTime')} hint={t('checkinScheduleHint')}>
                    <div className="space-y-2">
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-caption text-muted">{t('checkinAutoMode')}</span>
                        <CompactSwitch
                          isSelected={checkinAuto}
                          isDisabled={saving}
                          ariaLabel={t('checkinAutoMode')}
                          onChange={setCheckinAuto}
                        />
                      </div>
                      <Input
                        type="time"
                        value={checkinAuto ? autoCheckinFallback(provider) : checkinTime}
                        onChange={(event) => setCheckinTime(event.target.value)}
                        aria-label={t('autoCheckinTime')}
                        disabled={saving || checkinAuto}
                        required={!checkinAuto}
                      />
                      {checkinTimezone ? <p className="text-caption text-muted">{t('checkinServerClock', { timezone: checkinTimezone })}</p> : null}
                      {checkinAuto ? <p className="text-caption text-muted">{t('checkinAutoFallbackHint')}</p> : null}
                    </div>
                  </FormRow>
                ) : null}
              </Form>
            </Modal.Body>
            <Modal.Footer className="justify-end gap-2 px-6 pb-6">
              <Button variant="ghost" isDisabled={saving} onPress={onClose}>{t('cancel')}</Button>
              <Button isPending={saving} onPress={() => void submit()}>{t('save')}</Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
