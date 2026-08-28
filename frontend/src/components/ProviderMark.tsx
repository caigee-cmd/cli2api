import { QoderMark } from '@/components/QoderMark'
import { TraeMark } from '@/components/TraeMark'
import { WorkBuddyMark } from '@/components/WorkBuddyMark'

type Props = {
  provider?: string
  size?: number
  className?: string
}

/** Brand mark for an account provider; unknown values fall back to Qoder. */
export function ProviderMark({ provider, size = 16, className = '' }: Props) {
  const id = String(provider || '').toLowerCase()
  if (id === 'workbuddy') {
    return <WorkBuddyMark size={size} className={className} />
  }
  if (id === 'trae') {
    return <TraeMark size={size} className={className} />
  }
  return <QoderMark size={size} className={className} />
}
