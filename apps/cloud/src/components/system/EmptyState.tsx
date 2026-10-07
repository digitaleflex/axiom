import type { ReactNode } from 'react'

export type EmptyStateVariant = 'first-use' | 'no-results' | 'not-deployed'

interface EmptyStateProps {
  title: string
  description: string
  variant?: EmptyStateVariant
  icon?: ReactNode
  primaryAction?: ReactNode
  secondaryAction?: ReactNode
}

/** docs/design/components/system §3.1. Screens pass content, never styling. */
export function EmptyState({
  title,
  description,
  variant = 'first-use',
  icon = '◇',
  primaryAction,
  secondaryAction,
}: EmptyStateProps) {
  return (
    <div className="empty-state" data-variant={variant}>
      <span className="empty-state__icon" aria-hidden="true">
        {icon}
      </span>
      <h2 className="empty-state__title">{title}</h2>
      <p className="empty-state__body">{description}</p>
      {(primaryAction || secondaryAction) && (
        <div className="empty-state__actions">
          {primaryAction}
          {secondaryAction}
        </div>
      )}
    </div>
  )
}
