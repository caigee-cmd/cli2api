import { useState } from 'react'
import { Alert, Button, Chip, Description, Form, Input, Label, Modal, NumberField } from '@heroui/react'
import { X } from '@phosphor-icons/react'
import { ProviderMark } from '@/components/ProviderMark'
import type { AccountRow } from '@/lib/account'
import { accountProviderLabel } from '@/lib/provider'

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  account: AccountRow | null
  busy: boolean
  t: Translate
  onClose: () => void
  onSave: (input: { name: string; max_inflight: number; priority: number }) => Promise<void>
}

export function EditAccountModal({ account, busy, t, onClose, onSave }: Props) {
  const [name, setName] = useState(account?.name || '')
  const [maxInFlight, setMaxInFlight] = useState<number>(account?.max_inflight ?? 4)
  const [priority, setPriority] = useState<number>(account?.priority ?? 50)
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
    setError('')
    try {
      await onSave({ name: trimmed, max_inflight: maxInFlight, priority })
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
              <Form className="space-y-5" onSubmit={(event) => void submit(event)}>
                <div className="space-y-1.5">
                  <Label className="text-sm font-medium text-muted">{t('accountName')}</Label>
                  <Input
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder={t('wizardNamePh')}
                    aria-label={t('accountName')}
                    disabled={busy}
                    autoFocus
                  />
                </div>
                <div className="grid gap-5 sm:grid-cols-2">
                  <NumberField
                    value={maxInFlight}
                    onChange={(value) => setMaxInFlight(value ?? 4)}
                    minValue={1}
                    maxValue={32}
                    isDisabled={busy}
                    isRequired
                  >
                    <Label className="text-sm font-medium text-muted">{t('maxInflight')}</Label>
                    <NumberField.Group>
                      <NumberField.DecrementButton />
                      <NumberField.Input />
                      <NumberField.IncrementButton />
                    </NumberField.Group>
                    <Description className="text-xs leading-5 text-muted">{t('maxInflightHint')}</Description>
                  </NumberField>
                  <NumberField
                    value={priority}
                    onChange={(value) => setPriority(value ?? 50)}
                    minValue={1}
                    maxValue={100}
                    isDisabled={busy}
                    isRequired
                  >
                    <Label className="text-sm font-medium text-muted">{t('priority')}</Label>
                    <NumberField.Group>
                      <NumberField.DecrementButton />
                      <NumberField.Input />
                      <NumberField.IncrementButton />
                    </NumberField.Group>
                    <Description className="text-xs leading-5 text-muted">{t('priorityHint')}</Description>
                  </NumberField>
                </div>
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
