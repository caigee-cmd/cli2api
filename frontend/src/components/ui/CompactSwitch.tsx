import { Label, Switch } from '@heroui/react'

type CompactSwitchProps = {
  isSelected: boolean
  isDisabled?: boolean
  ariaLabel: string
  label?: string
  size?: 'sm' | 'md' | 'lg'
  onChange: (selected: boolean) => void
}

export function CompactSwitch({
  isSelected,
  isDisabled,
  ariaLabel,
  label,
  size = 'sm',
  onChange,
}: CompactSwitchProps) {
  return (
    <Switch size={size} isSelected={isSelected} isDisabled={isDisabled} onChange={onChange}>
      <Switch.Content>
        <Switch.Control>
          <Switch.Thumb />
        </Switch.Control>
        {label ? <Label>{label}</Label> : <span className="sr-only">{ariaLabel}</span>}
      </Switch.Content>
    </Switch>
  )
}
