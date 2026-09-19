import { useState } from 'react'
import { Alert, Button, Chip, Form, Input, Modal, NumberField } from '@heroui/react'
import { X } from '@phosphor-icons/react'
import { ProviderMark } from '@/components/ProviderMark'
import { FormRow } from '@/components/ui/FormRow'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import type { AccountRow } from '@/lib/account'
import { accountProviderLabel } from '@/lib/provider'

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  account: AccountRow | null
  checkinDefaultTime?: string
  checkinTimezone?: string
  busy: boolean
  t: Translate
  onClose: () => void
  onSave: (input: { name: string; max_inflight: number; priority: number; proxy_url: string; checkin_time?: string; drop_system_prompt?: boolean }) => Promise<void>
}

export function EditAccountModal({ account, checkinDefaultTime, checkinTimezone, busy, t, onClose, onSave }: Props) {
  const [name, setName] = useState(account?.name || '')
  const [maxInFlight, setMaxInFlight] = useState<number>(account?.max_inflight ?? 4)
  const [priority, setPriority] = useState<number>(account?.priority ?? 50)
  const [proxyUrl, setProxyUrl] = useState(account?.proxy_url || '')
  const [dropSystemPrompt, setDropSystemPrompt] = useState(Boolean(account?.drop_system_prompt))
  const [inheritCheckinTime, setInheritCheckinTime] = useState(!account?.checkin_time)
  const [checkinTime, setCheckinTime] = useState<string | null>(account?.checkin_time || null)
  const [error, setError] = useState('')
  const title = t('editAccountTitle', { name: account?.name || account?.id || '' })
  const provider = account ? accountProviderLabel(account.provider, account.region, t) : ''

  async function submit(event?: { preventDefault(): void }) {
    event?.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setError(t('accountNameRequired'))
      return
    }
    if (!Number.isInteger(maxInFlight) || maxInFlight < 1 || maxInFlight > 32) {
      setError(t('maxInflightInvalid'))
      return
    }
    if (!Number.isInteger(priority) || priority < 1 || priority > 100) {
      setError(t('priorityInvalid'))
      return
    }
    if (checkinDefaultTime !== undefined && !inheritCheckinTime && !/^([01]\d|2[0-3]):[0-5]\d$/.test(checkinTime ?? checkinDefaultTime)) {
      setError(t('checkinTimeInvalid'))
      return
    }
    setError('')
    try {
      await onSave({
        name: trimmed,
        max_inflight: maxInFlight,
        priority,
        proxy_url: proxyUrl.trim(),
        checkin_time: checkinDefaultTime !== undefined ? (inheritCheckinTime ? '' : checkinTime ?? checkinDefaultTime) : undefined,
        drop_system_prompt: account?.provider === 'workbuddy' ? dropSystemPrompt : undefined,
      })
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Modal.Root isOpen={Boolean(account)} onOpenChange={(next: boolean) => { if (!next && !busy) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container placement="center" size="lg" scroll="inside">
          <Modal.Dialog className="sm:min-w-[32rem]">
            <Modal.Header className="items-start justify-between gap-4 px-6 pt-6">
              <div className="min-w-0">
                <Modal.Heading className="text-lg font-semibold tracking-[-0.015em]">{title}</Modal.Heading>
                <p className="mt-1.5 text-sm font-normal leading-6 text-muted">{t('editAccountHint')}</p>
                {account ? (
                  <div className="mt-3 flex flex-wrap items-center gap-2">
                    <Chip size="sm" variant="soft">
                      <span className="flex items-center gap-1.5">
                        <ProviderMark provider={account.provider} size={14} />
                        <span>{provider}</span>
                      </span>
                    </Chip>
                    <span className="mono text-xs text-muted">{account.id}</span>
                  </div>
                ) : null}
              </div>
              <Modal.CloseTrigger isDisabled={busy} aria-label={t('close')} className="grid size-9 shrink-0 place-items-center rounded-lg text-muted hover:bg-surface-secondary">
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
              <Form className="space-y-4" onSubmit={(event) => void submit(event)}>
                <FormRow label={t('accountName')}>
                  <Input
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder={t('wizardNamePh')}
                    aria-label={t('accountName')}
                    disabled={busy}
                    autoFocus
                  />
                </FormRow>
                <FormRow label={t('maxInflight')} hint={t('maxInflightHint')}>
                  <NumberField
                    value={maxInFlight}
                    onChange={(value) => setMaxInFlight(value ?? 4)}
                    minValue={1}
                    maxValue={32}
                    isDisabled={busy}
                    isRequired
                  >
                    <NumberField.Group>
                      <NumberField.DecrementButton />
                      <NumberField.Input aria-label={t('maxInflight')} />
                      <NumberField.IncrementButton />
                    </NumberField.Group>
                  </NumberField>
                </FormRow>
                <FormRow label={t('priority')} hint={t('priorityHint')}>
                  <NumberField
                    value={priority}
                    onChange={(value) => setPriority(value ?? 50)}
                    minValue={1}
                    maxValue={100}
                    isDisabled={busy}
                    isRequired
                  >
                    <NumberField.Group>
                      <NumberField.DecrementButton />
                      <NumberField.Input aria-label={t('priority')} />
                      <NumberField.IncrementButton />
                    </NumberField.Group>
                  </NumberField>
                </FormRow>
                <FormRow label={t('proxyUrl')} hint={t('proxyUrlHint')}>
                  <Input
                    value={proxyUrl}
                    onChange={(event) => setProxyUrl(event.target.value)}
                    placeholder={t('proxyUrlPlaceholder')}
                    aria-label={t('proxyUrl')}
                    disabled={busy}
                  />
                </FormRow>
                {account?.provider === 'workbuddy' ? (
                  <FormRow label={t('dropSystemPrompt')} hint={t('dropSystemPromptHint')}>
                    <CompactSwitch
                      isSelected={dropSystemPrompt}
                      isDisabled={busy}
                      ariaLabel={t('dropSystemPrompt')}
                      onChange={setDropSystemPrompt}
                    />
                  </FormRow>
                ) : null}
                {checkinDefaultTime !== undefined ? (
                  <FormRow label={t('autoCheckinTime')} hint={t('checkinScheduleHint')}>
                    <div className="space-y-3">
                      <CompactSwitch isSelected={inheritCheckinTime} isDisabled={busy} onChange={setInheritCheckinTime} ariaLabel={t('checkinInherit')} label={t('checkinInherit')} />
                      <p className="text-xs text-muted">{t('checkinDefaultValue', { time: checkinDefaultTime })} · <span className="mono">{checkinTimezone}</span></p>
                      {!inheritCheckinTime ? <Input type="time" value={checkinTime ?? checkinDefaultTime} onChange={(event) => setCheckinTime(event.target.value)} aria-label={t('autoCheckinTime')} disabled={busy} required /> : null}
                    </div>
                  </FormRow>
                ) : null}
              </Form>
            </Modal.Body>
            <Modal.Footer className="justify-end gap-2 px-6 pb-6">
              <Button variant="ghost" isDisabled={busy} onPress={onClose}>{t('cancel')}</Button>
              <Button isPending={busy} onPress={() => void submit()}>{t('save')}</Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
