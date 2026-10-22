import { useCallback, useEffect, useState } from 'react'
import { applyPreparedSystemUpdate, cancelSystemUpdate, fetchSystemUpdate, rollbackSystemUpdate, startSystemUpdate, type StartUpdateResult, type SystemUpdateInfo } from '@/api/system'
import { useI18n } from '@/hooks/useI18n'

const busyStates = new Set(['preparing', 'preparing_image', 'checking', 'backing_up', 'submitting', 'running', 'queued', 'pulling', 'host_binary', 'image_ready', 'recreating', 'rolling_back'])
const applyJobStates = new Set(['backing_up', 'running'])
const applyAgentStates = new Set(['recreating', 'rolling_back'])
const progressAgentStates = new Set(['queued', 'pulling', 'host_binary', 'image_ready', 'preparing', 'recreating', 'checking', 'rolling_back'])

/** Shared system update state machine (settings 版本更新 tab and the sidebar popup). */
export function useSystemUpdate() {
  const { t } = useI18n()
  const [info, setInfo] = useState<SystemUpdateInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [checking, setChecking] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [started, setStarted] = useState<StartUpdateResult | null>(null)
  const [reloadIn, setReloadIn] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const [initialVersion, setInitialVersion] = useState('')

  const load = useCallback(async (force = false, quiet = false) => {
    if (force && !quiet) setChecking(true)
    try {
      const result = await fetchSystemUpdate(force)
      setInfo(result)
      setInitialVersion((current) => current || result.current_version || '')
      const failed = result.update?.state === 'failed' || result.update?.state === 'rolled_back' || result.agent?.state === 'failed' || result.agent?.state === 'rolled_back'
      if (failed) {
        setError(result.update?.error || result.agent?.error || t('updateFailedHint'))
      } else if (!quiet) {
        setError('')
      }
    } catch (err) {
      if (!quiet) setError(err instanceof Error ? err.message : t('updateCheckFailedHint'))
    } finally {
      setLoading(false)
      if (force && !quiet) setChecking(false)
    }
  }, [t])

  useEffect(() => {
    const timer = window.setTimeout(() => void load(false), 0)
    return () => window.clearTimeout(timer)
  }, [load])

  const agentState = info?.agent?.state || 'unavailable'
  const preparationState = info?.update?.state || ''
  const applying = applyJobStates.has(preparationState) || applyAgentStates.has(agentState) || Boolean(info?.update?.backup_path && busyStates.has(preparationState))
  const readyToApply = !applying && (preparationState === 'ready_to_apply' || agentState === 'ready_to_apply')
  const waitingForJob = Boolean(started?.job_id) && info?.update?.job_id !== started?.job_id && !readyToApply && !applying
  const preparing = !applying && !readyToApply && (busyStates.has(preparationState) || busyStates.has(agentState) || waitingForJob)
  const busy = preparing || applying
  const succeeded = preparationState === 'succeeded' || agentState === 'succeeded'
  const justUpdated = Boolean(succeeded && initialVersion && info?.current_version && info.current_version !== initialVersion)
  if (justUpdated && reloadIn == null) setReloadIn(3)
  const targetVersion = info?.update?.target_version || info?.agent?.target_version || ''
  const newerThanPrepared = Boolean(readyToApply && info?.next_version && targetVersion && info.next_version !== targetVersion)
  const startedAt = info?.update?.started_at || info?.agent?.started_at
  const elapsed = startedAt && busy ? Math.max(0, Math.round((now - Date.parse(startedAt)) / 1000)) : 0
  const visibleState = justUpdated
    ? 'succeeded'
    : readyToApply
      ? 'ready_to_apply'
      : busy && progressAgentStates.has(agentState)
        ? agentState
        : (preparationState || agentState)
  const updateStateLabel = visibleState ? t(`updateState_${visibleState}`) : ''
  const updateStateText = updateStateLabel.startsWith('updateState_') ? visibleState : updateStateLabel
  useEffect(() => {
    if (reloadIn != null || (!busy && !justUpdated)) return
    const timer = window.setInterval(() => void load(false, true), 2000)
    return () => window.clearInterval(timer)
  }, [busy, justUpdated, load, reloadIn])

  useEffect(() => {
    if (!busy) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [busy])

  useEffect(() => {
    if (reloadIn == null) return
    if (reloadIn <= 0) {
      window.location.reload()
      return
    }
    const timer = window.setTimeout(() => setReloadIn((current) => (current == null ? current : current - 1)), 1000)
    return () => window.clearTimeout(timer)
  }, [reloadIn])

  const canPrepare = Boolean(info?.managed && info?.has_update && info?.next_version && info?.agent?.available && !readyToApply && !busy && !submitting && reloadIn == null)
  const canApply = Boolean(readyToApply && info?.agent?.available && !applying && !submitting && reloadIn == null)
  const canCancel = Boolean(info?.agent?.available && (preparing || readyToApply) && !applying && !submitting && reloadIn == null)
  const canRollback = Boolean(info?.managed && info?.agent?.available && (info.rollback_versions?.length ?? 0) > 0 && !readyToApply && !busy && !submitting && reloadIn == null)
  const primaryLabel = reloadIn != null
    ? t('updateReloadingIn', { seconds: reloadIn })
    : canApply
      ? t('applyUpdateNow')
      : applying
        ? t('updateInProgress')
        : preparing || submitting
          ? t('updatePreparingImage')
          : t('updateNow')
  const statusHint = reloadIn != null
    ? t('updateReloadingHint')
    : readyToApply
      ? t('updateReadyTargetHint', { version: targetVersion || info?.next_version || '' })
      : applying
        ? t('updateApplyingHint')
        : t('updatePreparingImageHint')

  async function prepareUpdate() {
    setSubmitting(true)
    setStarted(null)
    setError('')
    try {
      const result = await startSystemUpdate()
      setStarted(result)
      await load(false, true)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('updateFailedHint'))
      await load(false, true)
    } finally {
      setSubmitting(false)
    }
  }

  async function confirmUpdate() {
    setSubmitting(true)
    setError('')
    try {
      const result = await applyPreparedSystemUpdate()
      setStarted(result)
      await load(false, true)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('updateFailedHint'))
      await load(false, true)
    } finally {
      setSubmitting(false)
    }
  }

  async function rollbackTo(version: string) {
    setSubmitting(true)
    setStarted(null)
    setError('')
    try {
      const result = await rollbackSystemUpdate(version)
      setStarted(result)
      await load(false, true)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('updateFailedHint'))
      await load(false, true)
    } finally {
      setSubmitting(false)
    }
  }

  async function cancelPreparedUpdate() {
    setSubmitting(true)
    setError('')
    try {
      await cancelSystemUpdate()
      setStarted(null)
      await load(true, true)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('updateFailedHint'))
      await load(false, true)
    } finally {
      setSubmitting(false)
    }
  }

  return {
    info, loading, checking, submitting, error, setError, reloadIn, load, elapsed,
    readyToApply, busy, justUpdated, targetVersion, newerThanPrepared, updateStateText,
    canPrepare, canApply, canCancel, canRollback, primaryLabel, statusHint,
    prepareUpdate, confirmUpdate, cancelPreparedUpdate, rollbackTo,
  }
}

export type SystemUpdateController = ReturnType<typeof useSystemUpdate>
