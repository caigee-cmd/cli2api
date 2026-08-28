import { useState } from 'react'
import { Button, Input, Modal } from '@heroui/react'
import { WarningCircle, X } from '@phosphor-icons/react'
import { ProviderMark } from '@/components/ProviderMark'
import type { AccountRow } from '@/lib/account'
import { accountProviderLabel } from '@/lib/provider'

const ACCOUNT_BUTTON_CLASS = 'account-button'

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
  const [maxInFlight, setMaxInFlight] = useState(String(account?.max_inflight ?? 4))
  const [priority, setPriority] = useState(String(account?.priority ?? 50))
  const [error, setError] = useState('')
  const title = t('editAccountTitle', { name: account?.name || account?.id || '' })
  const provider = account ? accountProviderLabel(account.provider, account.region, t) : ''

  async function submit() {
    const trimmed = name.trim()
    if (!trimmed) {
      setError(t('accountNameRequired'))
      return
    }
    const nextInFlight = Number(maxInFlight)
    if (!Number.isInteger(nextInFlight) || nextInFlight < 1 || nextInFlight > 32) {
      setError(t('maxInflightInvalid'))
      return
    }
    const nextPriority = Number(priority)
    if (!Number.isInteger(nextPriority) || nextPriority < 1 || nextPriority > 100) {
      setError(t('priorityInvalid'))
      return
    }
    setError('')
    try {
      await onSave({ name: trimmed, max_inflight: nextInFlight, priority: nextPriority })
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Modal.Root isOpen={Boolean(account)} onOpenChange={(next: boolean) => { if (!next && !busy) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container placement="center" size="sm">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4">
              <div className="min-w-0">
                <Modal.Heading className="text-base font-semibold">{title}</Modal.Heading>
                <p className="mt-1 text-xs font-normal leading-5 text-[var(--app-faint)]">{t('editAccountHint')}</p>
                {account ? (
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px] text-[var(--app-muted)]">
                    <ProviderMark provider={account.provider} size={14} />
                    <span>{provider}</span>
                    <span className="text-[var(--app-line-strong)]">·</span>
                    <span className="mono">{account.id}</span>
                  </div>
                ) : null}
              </div>
              <Modal.CloseTrigger isDisabled={busy} aria-label={t('close')} className="grid size-8 shrink-0 place-items-center rounded-lg text-[var(--app-muted)] hover:bg-[var(--app-surface-muted)]">
                <X size={16} />
              </Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="pt-0">
              {error ? (
                <div className="mb-3 flex gap-2 rounded-lg border border-[color-mix(in_srgb,var(--app-danger)_24%,transparent)] bg-[color-mix(in_srgb,var(--app-danger)_7%,transparent)] px-3 py-2.5 text-xs leading-5 text-[var(--app-danger)]">
                  <WarningCircle size={14} className="mt-0.5 shrink-0" />
                  <span>{error}</span>
                </div>
              ) : null}
              <div className="space-y-3">
                <label className="block space-y-1.5">
                  <span className="text-xs font-medium text-[var(--app-muted)]">{t('accountName')}</span>
                  <Input
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder={t('wizardNamePh')}
                    aria-label={t('accountName')}
                    disabled={busy}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') void submit()
                    }}
                  />
                </label>
                <div className="grid gap-3 sm:grid-cols-2">
                  <label className="block space-y-1.5">
                    <span className="text-xs font-medium text-[var(--app-muted)]">{t('maxInflight')}</span>
                    <Input
                      type="number"
                      min={1}
                      max={32}
                      value={maxInFlight}
                      onChange={(event) => setMaxInFlight(event.target.value)}
                      aria-label={t('maxInflight')}
                      disabled={busy}
                    />
                    <p className="text-[11px] leading-4 text-[var(--app-faint)]">{t('maxInflightHint')}</p>
                  </label>
                  <label className="block space-y-1.5">
                    <span className="text-xs font-medium text-[var(--app-muted)]">{t('priority')}</span>
                    <Input
                      type="number"
                      min={1}
                      max={100}
                      value={priority}
                      onChange={(event) => setPriority(event.target.value)}
                      aria-label={t('priority')}
                      disabled={busy}
                    />
                    <p className="text-[11px] leading-4 text-[var(--app-faint)]">{t('priorityHint')}</p>
                  </label>
                </div>
              </div>
            </Modal.Body>
            <Modal.Footer className="justify-end">
              <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="ghost" isDisabled={busy} onPress={onClose}>{t('cancel')}</Button>
              <Button className={ACCOUNT_BUTTON_CLASS} size="sm" isPending={busy} onPress={() => void submit()}>{t('save')}</Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
