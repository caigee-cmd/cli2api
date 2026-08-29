import { Alert } from '@heroui/react'

export function PageAlert({
  status = 'danger',
  title,
  description,
}: {
  status?: 'default' | 'accent' | 'success' | 'warning' | 'danger'
  title: string
  description?: string
}) {
  return (
    <Alert status={status}>
      <Alert.Indicator />
      <Alert.Content>
        <Alert.Title>{title}</Alert.Title>
        {description ? <Alert.Description>{description}</Alert.Description> : null}
      </Alert.Content>
    </Alert>
  )
}
