import type { ReactNode } from 'react'

export type BannerVariant = 'offline' | 'degraded' | 'warning'

interface PageBannerProps {
  variant?: BannerVariant
  children: ReactNode
  actions?: ReactNode
}

/** Page-wide banner rendered under the context bar (system §3.2, shell §9). */
export function PageBanner({ variant = 'warning', children, actions }: PageBannerProps) {
  return (
    <div className={`page-banner page-banner--${variant}`} role="alert">
      <span className="page-banner__icon" aria-hidden="true">
        ▲
      </span>
      <div>{children}</div>
      {actions}
    </div>
  )
}
