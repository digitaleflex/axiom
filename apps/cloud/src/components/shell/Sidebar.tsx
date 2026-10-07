import { NavLink } from 'react-router-dom'
import { routes, type ApplicationSection } from '../../routes/builders'
import type { RouteContext, SidebarKey } from '../../routes/context'

interface NavItem {
  key: SidebarKey
  label: string
  to: string
  icon: string
  disabled?: boolean
  disabledReason?: string
}

const WORKSPACE_ITEMS: NavItem[] = [
  { key: 'dashboard', label: 'Dashboard', to: routes.dashboard(), icon: '▦' },
  { key: 'repositories', label: 'Repositories', to: routes.repositories(), icon: '⧉' },
  { key: 'github', label: 'GitHub', to: routes.github(), icon: '⌥' },
]

const APPLICATION_ITEMS: NavItem[] = [
  { key: 'overview', label: 'Overview', to: '', icon: '◎' },
  { key: 'deployments', label: 'Deployments', to: '', icon: '⇧' },
  { key: 'logs', label: 'Logs', to: '', icon: '≡' },
  { key: 'metrics', label: 'Metrics', to: '', icon: '∿' },
  { key: 'app-domains', label: 'Domains', to: '', icon: '◇' },
]

const INFRASTRUCTURE_ITEMS: NavItem[] = [
  { key: 'servers', label: 'Servers', to: routes.servers(), icon: '▤' },
  { key: 'domains', label: 'Domains', to: routes.domains(), icon: '◇' },
]

interface SidebarProps {
  ctx: RouteContext
  applicationName?: string
  open: boolean
  collapsed: boolean
  onToggleCollapse: () => void
  onNavigate: () => void
}

export function Sidebar({
  ctx,
  applicationName,
  open,
  collapsed,
  onToggleCollapse,
  onNavigate,
}: SidebarProps) {
  const applicationBase = ctx.applicationId
    ? { applicationId: ctx.applicationId, environment: ctx.environment ?? 'production' }
    : null

  const applicationItems: NavItem[] = applicationBase
    ? APPLICATION_ITEMS.map((item) => ({
        ...item,
        to: routes.appSection({
          ...applicationBase,
          section: (item.key === 'app-domains' ? 'domains' : item.key) as ApplicationSection,
        }),
        disabled: !ctx.environment,
        disabledReason: ctx.environment ? undefined : 'Available after the first deployment',
      }))
    : []

  return (
    <nav
      aria-label="Primary"
      className={`sidebar${open ? ' sidebar--open' : ''}`}
      onClick={onNavigate}
    >
      <div className="sidebar__brand">
        <span className="sidebar__mark">A</span>
        <span>AXIOM</span>
      </div>

      <div className="sidebar__nav">
        <div className="sidebar__group">
          <div className="sidebar__group-label">Workspace</div>
          {WORKSPACE_ITEMS.map((item) => (
            <NavItemLink key={item.key} item={item} active={ctx.sidebarActive === item.key} />
          ))}
        </div>

        {applicationBase && (
          <div className="sidebar__group">
            <div className="sidebar__group-label">Application</div>
            <div className="sidebar__app-name" title={applicationName ?? ctx.applicationId}>
              <span>{applicationName ?? ctx.applicationId}</span>
              <span aria-hidden="true">▾</span>
            </div>
            {applicationItems.map((item) => (
              <NavItemLink key={item.key} item={item} active={ctx.sidebarActive === item.key} />
            ))}
          </div>
        )}

        <div className="sidebar__group">
          <div className="sidebar__group-label">Infrastructure</div>
          {INFRASTRUCTURE_ITEMS.map((item) => (
            <NavItemLink key={item.key} item={item} active={ctx.sidebarActive === item.key} />
          ))}
        </div>
      </div>

      <div className="sidebar__footer">
        <NavItemLink
          item={{ key: 'settings', label: 'Settings', to: routes.settings(), icon: '⚙' }}
          active={ctx.sidebarActive === 'settings'}
        />
        <div className="sidebar__user">
          <span className="avatar" aria-hidden="true">
            JD
          </span>
          <span>Local operator</span>
        </div>
        <button type="button" className="btn btn--ghost" onClick={onToggleCollapse}>
          {collapsed ? 'Expand' : 'Collapse'}
        </button>
      </div>
    </nav>
  )
}

function NavItemLink({ item, active }: { item: NavItem; active: boolean }) {
  if (item.disabled) {
    return (
      <span
        className="sidebar__item"
        aria-disabled="true"
        title={item.disabledReason}
      >
        <span className="sidebar__icon" aria-hidden="true">
          {item.icon}
        </span>
        <span>{item.label}</span>
      </span>
    )
  }

  return (
    <NavLink
      to={item.to}
      className={`sidebar__item${active ? ' sidebar__item--active' : ''}`}
      aria-current={active ? 'page' : undefined}
    >
      <span className="sidebar__icon" aria-hidden="true">
        {item.icon}
      </span>
      <span>{item.label}</span>
    </NavLink>
  )
}
