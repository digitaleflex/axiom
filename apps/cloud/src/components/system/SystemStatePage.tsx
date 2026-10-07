import type { ReactNode } from 'react'

export type SystemStateCode = '404' | '403' | '500' | '503' | 'offline'

interface SystemStatePageProps {
  code: SystemStateCode
  title: string
  description: string
  actions?: ReactNode
  detail?: string
}

/** Full-page state rendered inside the shell (system-states §1). */
export function SystemStatePage({ code, title, description, actions, detail }: SystemStatePageProps) {
  return (
    <div className="system-state" role="alert">
      <div className="system-state__code">{code}</div>
      <h1 className="system-state__title">{title}</h1>
      <p className="system-state__body">{description}</p>
      {actions && <div className="system-state__actions">{actions}</div>}
      {detail && <div className="system-state__code">{detail}</div>}
    </div>
  )
}
