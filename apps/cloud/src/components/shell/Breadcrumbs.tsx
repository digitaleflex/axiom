import { Fragment } from 'react'
import { Link } from 'react-router-dom'
import type { Environment } from '../../routes/builders'
import { EnvironmentChip } from './EnvironmentChip'

export interface Crumb {
  label: string
  to?: string
  current?: boolean
  environment?: Environment
}

/** Context hierarchy, not browsing history (navigation §8). */
export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumb" className="breadcrumbs">
      {items.map((crumb, index) => (
        <Fragment key={`${crumb.label}-${index}`}>
          {index > 0 && (
            <span className="breadcrumbs__sep" aria-hidden="true">
              ›
            </span>
          )}
          {crumb.environment ? (
            <EnvironmentChip environment={crumb.environment} size="sm" abbreviated />
          ) : crumb.to && !crumb.current ? (
            <Link to={crumb.to}>{crumb.label}</Link>
          ) : (
            <span aria-current={crumb.current ? 'page' : undefined}>{crumb.label}</span>
          )}
        </Fragment>
      ))}
    </nav>
  )
}
