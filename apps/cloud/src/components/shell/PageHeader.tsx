import type { ReactNode } from 'react'
import type { Environment } from '../../routes/builders'
import { Breadcrumbs, type Crumb } from './Breadcrumbs'
import { EnvironmentChip } from './EnvironmentChip'

interface PageHeaderProps {
  breadcrumbs: Crumb[]
  title: string
  environment?: Environment
  notDeployed?: boolean
  meta?: ReactNode
  actions?: ReactNode
  tabs?: ReactNode
}

/** Page header region (screens/shell §6). */
export function PageHeader({
  breadcrumbs,
  title,
  environment,
  notDeployed,
  meta,
  actions,
  tabs,
}: PageHeaderProps) {
  return (
    <header className="page-header">
      <Breadcrumbs items={breadcrumbs} />
      <div className="page-header__title-row">
        <h1 className="page-header__title">{title}</h1>
        {environment && <EnvironmentChip environment={environment} notDeployed={notDeployed} />}
        {actions && <div className="page-header__actions">{actions}</div>}
      </div>
      {meta && <div className="page-header__meta">{meta}</div>}
      {tabs}
    </header>
  )
}
