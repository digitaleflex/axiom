import { useState, type ReactNode } from 'react'

export type NoticeVariant = 'success' | 'info' | 'warning' | 'degraded' | 'failed' | 'partial'

const ICONS: Record<NoticeVariant, string> = {
  success: '✓',
  info: 'ⓘ',
  warning: '▲',
  degraded: '◐',
  failed: '⬢',
  partial: '◧',
}

interface InlineNoticeProps {
  variant?: NoticeVariant
  title: string
  children?: ReactNode
  actions?: ReactNode
  dismissible?: boolean
  onDismiss?: () => void
}

/** docs/design/components/system §3.2. Inline within a section. */
export function InlineNotice({
  variant = 'info',
  title,
  children,
  actions,
  dismissible = false,
  onDismiss,
}: InlineNoticeProps) {
  const [dismissed, setDismissed] = useState(false)
  if (dismissed) return null

  return (
    <div
      className={`notice notice--${variant}`}
      role={variant === 'failed' ? 'alert' : 'status'}
    >
      <span className="notice__icon" aria-hidden="true">
        {ICONS[variant]}
      </span>
      <div className="notice__content">
        <div className="notice__title">{title}</div>
        {children && <div className="notice__body">{children}</div>}
        {actions && <div className="notice__actions">{actions}</div>}
      </div>
      {dismissible && (
        <button
          type="button"
          className="notice__close"
          aria-label="Dismiss"
          onClick={() => {
            setDismissed(true)
            onDismiss?.()
          }}
        >
          ×
        </button>
      )}
    </div>
  )
}
