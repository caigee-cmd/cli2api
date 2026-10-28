import { useI18n } from '@/hooks/useI18n'

const CAIAI_HOME_URL = 'https://caiaiu.com'

/** Skipable hosted-gateway soft link for empty account states. Not a provider tile. */
export function CaiaiSoftLink({ className = '' }: { className?: string }) {
  const { t } = useI18n()
  return (
    <p className={`text-caption leading-5 text-muted ${className}`.trim()}>
      <a
        href={CAIAI_HOME_URL}
        target="_blank"
        rel="noopener noreferrer"
        className="underline-offset-2 transition-colors hover:text-foreground hover:underline"
      >
        {t('caiaiSoftLink')}
      </a>
    </p>
  )
}
