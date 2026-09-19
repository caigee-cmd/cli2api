import { useEffect, useState } from 'react'
import { Button, Card, Input } from '@heroui/react'
import { useNavigate } from 'react-router-dom'
import { fetchProviders, type ProviderDescriptor } from '@/api/overview'
import { updateSystemSettings, type SystemSettings } from '@/api/system'
import { FormRow } from '@/components/ui/FormRow'
import { PageAlert } from '@/components/ui/PageAlert'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import { useI18n } from '@/hooks/useI18n'

export function CheckinDefaults({ settings, onSaved }: { settings: SystemSettings | null; onSaved: (settings: SystemSettings) => void }) {
  const { t } = useI18n()
  const navigate = useNavigate()
  const [providers, setProviders] = useState<ProviderDescriptor[] | null>(null)
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    void fetchProviders().then((result) => {
      if (active) setProviders((result.data || []).filter((provider) => provider.regions.some((region) => region.checkin)))
    }).catch((err) => { if (active) setError(String(err)) })
    return () => { active = false }
  }, [])

  async function save(providerID: string, value: string) {
    if (!settings || value === settings.checkin_times[providerID]) return
    setBusy(true)
    setError('')
    try {
      onSaved(await updateSystemSettings({ checkin_times: { [providerID]: value } }))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setDrafts((current) => {
        const next = { ...current }
        delete next[providerID]
        return next
      })
      setBusy(false)
    }
  }

  return (
    <Card data-gsap-reveal>
      <Card.Header>
        <Card.Title>{t('checkinDefaultsTitle')}</Card.Title>
        <Card.Description>{t('checkinDefaultsHint')}</Card.Description>
      </Card.Header>
      <Card.Content className="space-y-4">
        {error ? <PageAlert title={error} /> : null}
        {!settings || !providers ? <SkeletonBlock className="h-24 w-full" /> : providers.map((provider) => (
          <FormRow key={provider.id} label={provider.label}
            hint={provider.regions.filter((region) => region.checkin).map((region) => `${region.label} · ${region.checkin?.timezone === 'Local' ? settings.timezone : region.checkin?.timezone}`).join(' / ')}>
            <Input type="time" value={drafts[provider.id] ?? settings.checkin_times[provider.id] ?? ''} disabled={busy}
              aria-label={`${provider.label} ${t('autoCheckinTime')}`}
              onChange={(event) => setDrafts((current) => ({ ...current, [provider.id]: event.target.value }))}
              onBlur={(event) => void save(provider.id, event.target.value)} />
          </FormRow>
        ))}
      </Card.Content>
      <Card.Footer><Button variant="secondary" onPress={() => navigate('/accounts')}>{t('navAccounts')}</Button></Card.Footer>
    </Card>
  )
}
