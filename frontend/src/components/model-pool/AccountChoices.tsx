import { useMemo, useState } from 'react'
import { Button, Checkbox, SearchField } from '@heroui/react'
import { X } from '@phosphor-icons/react'
import type { AccountRow } from '@/components/model-pool/options'

type Translate = (key: string, vars?: Record<string, string | number>) => string

export function AccountChoices({
  accounts,
  selected,
  busy,
  t,
  onChange,
}: {
  accounts: AccountRow[]
  selected: string[]
  busy?: boolean
  t: Translate
  onChange: (ids: string[]) => void
}) {
  const [query, setQuery] = useState('')
  const byId = useMemo(() => new Map(accounts.map((account) => [account.id, account])), [accounts])
  const selectedSet = useMemo(() => new Set(selected), [selected])
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    return accounts.filter((account) => {
      if (!q) return true
      const name = `${account.name || ''} ${account.id} ${account.region || ''}`.toLowerCase()
      return name.includes(q)
    })
  }, [accounts, query])
  const missing = selected.filter((id) => !byId.has(id))

  function toggle(id: string, on: boolean) {
    onChange(on ? [...selected, id] : selected.filter((item) => item !== id))
  }

  return (
    <div className="space-y-2">
      {selected.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {selected.map((id) => (
            <Button key={id} size="sm" variant="secondary" isDisabled={busy} onPress={() => toggle(id, false)}>
              <span className="max-w-40 truncate">{byId.get(id)?.name || id}</span>
              <X size={12} />
            </Button>
          ))}
        </div>
      ) : (
        <p className="text-caption text-muted">{t('modelPoolAccountsAll')}</p>
      )}
      {accounts.length > 6 ? (
        <SearchField aria-label={t('modelPoolAccountsSearch')} value={query} onChange={setQuery} isDisabled={busy}>
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder={t('modelPoolAccountsSearch')} />
            <SearchField.ClearButton />
          </SearchField.Group>
        </SearchField>
      ) : null}
      <div className="max-h-56 overflow-y-auto overscroll-contain rounded-lg bg-surface-secondary p-1">
        {missing.map((id) => (
          <div key={`missing-${id}`} className="flex items-center gap-2 px-2.5 py-1.5">
            <span className="mono min-w-0 flex-1 truncate text-caption text-muted">{id}</span>
            <Button isIconOnly size="sm" variant="ghost" isDisabled={busy} aria-label={t('modelPoolRemoveSeat')} onPress={() => toggle(id, false)}>
              <X size={14} />
            </Button>
          </div>
        ))}
        {visible.map((account) => (
          <Checkbox
            key={account.id}
            isSelected={selectedSet.has(account.id)}
            isDisabled={busy}
            onChange={(on) => toggle(account.id, on)}
            className="flex w-full cursor-pointer rounded-md px-2.5 py-1.5 hover:bg-default"
          >
            <Checkbox.Content className="flex min-w-0 flex-1 items-center gap-2.5">
              <Checkbox.Control><Checkbox.Indicator /></Checkbox.Control>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-body">{account.name || account.id}</span>
                <span className="mono block truncate text-caption text-muted">{account.region || account.id}</span>
              </span>
            </Checkbox.Content>
          </Checkbox>
        ))}
        {accounts.length === 0 ? <p className="px-2.5 py-2 text-caption text-muted">{t('modelPoolNoAccounts')}</p> : null}
        {accounts.length > 0 && visible.length === 0 ? <p className="px-2.5 py-2 text-caption text-muted">{t('modelPoolAccountsNoMatch')}</p> : null}
      </div>
    </div>
  )
}
