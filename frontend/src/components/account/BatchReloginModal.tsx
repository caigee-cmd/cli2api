import { useEffect, useRef, useState } from 'react'
import { Button, Chip, Modal } from '@heroui/react'
import { fetchLoginStatus, startDeviceLogin } from '@/api/overview'
import type { AccountRow } from '@/lib/account'
import { ProviderMark } from '@/components/ProviderMark'

type Translate = (key: string, vars?: Record<string, string | number>) => string

export type BatchLoginPhase = 'pending' | 'starting' | 'waiting' | 'ok' | 'error' | 'skipped' | 'stopped'

export type BatchLoginRow = {
  account: AccountRow
  phase: BatchLoginPhase
  message: string
  authUrl: string
}

const POLL_INTERVAL_MS = 2000
const ACCOUNT_TIMEOUT_MS = 5 * 60 * 1000

/**
 * Re-login several accounts one after another. Each account gets its own
 * authorization page: the console starts the flow, the operator authorizes
 * (an already signed-in browser does it silently), and polling moves on to
 * the next account. Accounts are never started in parallel on purpose — several
 * WorkBuddy accounts often share one SSO session, and parallel flows would hand
 * the same fresh token to different account records.
 */
export function BatchReloginModal({
  isOpen,
  accounts,
  t,
  onClose,
  onFinished,
}: {
  isOpen: boolean
  accounts: AccountRow[]
  t: Translate
  onClose: () => void
  onFinished: () => void
}) {
  const [rows, setRows] = useState<BatchLoginRow[]>([])
  const cancelled = useRef(false)
  const running = useRef(false)

  useEffect(() => {
    if (!isOpen) {
      cancelled.current = true
      running.current = false
      return
    }
    cancelled.current = false
    setRows(accounts.map((account) => ({ account, phase: 'pending', message: '', authUrl: '' })))
    if (running.current) return
    running.current = true
    void run(accounts)
    return () => {
      cancelled.current = true
      running.current = false
    }
    // accounts is the snapshot captured by the page when the dialog opened.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen])

  function patch(id: string, next: Partial<BatchLoginRow>) {
    setRows((current) => current.map((row) => (row.account.id === id ? { ...row, ...next } : row)))
  }

  async function run(list: AccountRow[]) {
    for (const account of list) {
      if (cancelled.current) {
        patch(account.id, { phase: 'stopped', message: t('reloginStopped') })
        continue
      }
      patch(account.id, { phase: 'starting', message: t('wizardStartingSession') })
      let authUrl = ''
      try {
        const output = await startDeviceLogin(account.id)
        authUrl = output.authUrl || ''
        patch(account.id, { authUrl, phase: 'waiting', message: output.message || t('reloginWaitingAuth') })
      } catch (error) {
        patch(account.id, { phase: 'error', message: error instanceof Error ? error.message : String(error) })
        continue
      }
      const deadline = Date.now() + ACCOUNT_TIMEOUT_MS
      let settled = false
      while (!settled) {
        if (cancelled.current) {
          patch(account.id, { phase: 'stopped', message: t('reloginStopped') })
          break
        }
        if (Date.now() > deadline) {
          patch(account.id, { phase: 'error', message: t('reloginTimeout') })
          break
        }
        await new Promise((resolve) => window.setTimeout(resolve, POLL_INTERVAL_MS))
        try {
          const status = await fetchLoginStatus(account.id)
          const login = status.login || {}
          const message = login.message || ''
          if (login.status === 'ok') {
            patch(account.id, { phase: 'ok', message: message || t('reloginDone') })
            settled = true
          } else if (login.status === 'error') {
            patch(account.id, { phase: 'error', message: message || t('reloginFailed') })
            settled = true
          } else if (message) {
            patch(account.id, { message })
          }
        } catch (error) {
          patch(account.id, { message: error instanceof Error ? error.message : String(error) })
        }
      }
    }
    running.current = false
    if (!cancelled.current) onFinished()
  }

  function stop() {
    cancelled.current = true
    running.current = false
    onClose()
  }

  const finished = rows.filter((row) => row.phase === 'ok' || row.phase === 'error' || row.phase === 'stopped').length

  return (
    <Modal.Root isOpen={isOpen} onOpenChange={(next: boolean) => { if (!next) stop() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="px-5 pt-5">
              <Modal.Heading className="text-section font-semibold">{t('reloginTitle')}</Modal.Heading>
              <p className="mt-1 text-caption font-normal leading-5 text-muted">{t('reloginHint')}</p>
            </Modal.Header>
            <Modal.Body className="space-y-2 px-5 pb-2">
              <div className="mono text-caption tabular-nums text-muted">
                {t('reloginProgress', { done: finished, total: rows.length })}
              </div>
              {rows.map((row) => (
                <div key={row.account.id} className="rounded-lg border border-divider bg-surface-secondary px-3 py-2">
                  <div className="flex flex-wrap items-center gap-2.5">
                    <ProviderMark provider={row.account.provider} size={16} />
                    <span className="min-w-0 flex-1 truncate text-body">{row.account.name || row.account.id}</span>
                    <Chip size="sm" variant="soft" color={phaseColor(row.phase)}>{t(phaseKey(row.phase))}</Chip>
                    {row.authUrl ? (
                      <Button size="sm" variant="secondary" onPress={() => window.open(row.authUrl, '_blank', 'noopener,noreferrer')}>
                        {t('reloginOpenPage')}
                      </Button>
                    ) : null}
                  </div>
                  {row.message ? <p className="mt-1 break-words text-caption text-foreground/70">{row.message}</p> : null}
                  {row.authUrl ? <p className="mono mt-1 truncate text-caption text-muted" title={row.authUrl}>{row.authUrl}</p> : null}
                </div>
              ))}
            </Modal.Body>
            <Modal.Footer className="justify-end px-5 pb-5">
              <Button variant="secondary" onPress={stop}>{t('cancel')}</Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}

function phaseKey(phase: BatchLoginPhase) {
  switch (phase) {
    case 'starting': return 'reloginPhaseStarting'
    case 'waiting': return 'reloginPhaseWaiting'
    case 'ok': return 'reloginPhaseDone'
    case 'error': return 'reloginPhaseFailed'
    case 'stopped': return 'reloginPhaseStopped'
    default: return 'reloginPhasePending'
  }
}

function phaseColor(phase: BatchLoginPhase) {
  if (phase === 'ok') return 'success'
  if (phase === 'error') return 'danger'
  if (phase === 'waiting' || phase === 'starting') return 'warning'
  return 'default'
}
