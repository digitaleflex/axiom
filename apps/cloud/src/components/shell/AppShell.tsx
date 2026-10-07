import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { getApplication } from '../../api/resources'
import type { Application } from '../../api/types'
import { useAsync } from '../../hooks/useAsync'
import { useOnline } from '../../hooks/useOnline'
import { parseRoute } from '../../routes/context'
import { PageBanner, ToastRegion } from '../system'
import { ActivityDrawer } from './ActivityDrawer'
import { ContextBar } from './ContextBar'
import { Sidebar } from './Sidebar'

const COLLAPSE_KEY = 'axiom.sidebarCollapsed'

function readCollapsed(): boolean {
  try {
    return window.sessionStorage.getItem(COLLAPSE_KEY) === '1'
  } catch {
    return false
  }
}

function titleFor(pathname: string, applicationName?: string, environment?: string): string {
  const ctx = parseRoute(pathname)
  const page =
    ctx.sidebarActive === 'dashboard'
      ? 'Dashboard'
      : ctx.sidebarActive === 'github'
        ? 'GitHub'
        : ctx.sidebarActive === 'repositories'
          ? 'Repositories'
          : ctx.sidebarActive === 'servers'
            ? 'Servers'
            : ctx.sidebarActive === 'domains' || ctx.sidebarActive === 'app-domains'
              ? 'Domains'
              : ctx.sidebarActive === 'settings'
                ? 'Settings'
                : ctx.sidebarActive === 'deployments'
                  ? 'Deployments'
                  : ctx.sidebarActive === 'logs'
                    ? 'Logs'
                    : ctx.sidebarActive === 'metrics'
                      ? 'Metrics'
                      : 'Overview'
  return [page, applicationName, environment, 'Axiom'].filter(Boolean).join(' · ')
}

/**
 * Application shell: skip link, sidebar, context bar, page content, activity
 * drawer and toast region (docs/design/screens/shell/README.md §1).
 */
export function AppShell() {
  const location = useLocation()
  const online = useOnline()
  const ctx = useMemo(() => parseRoute(location.pathname), [location.pathname])
  const mainRef = useRef<HTMLElement>(null)

  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(readCollapsed)

  const applicationState = useAsync<Application | null>(
    (signal) => (ctx.applicationId ? getApplication(ctx.applicationId, { signal }) : Promise.resolve(null)),
    [ctx.applicationId],
  )
  const applicationName = applicationState.data?.name

  const toggleCollapse = useCallback(() => {
    setCollapsed((value) => {
      const next = !value
      try {
        window.sessionStorage.setItem(COLLAPSE_KEY, next ? '1' : '0')
      } catch {
        // ignore storage failures
      }
      return next
    })
  }, [])

  // Route change: move focus to <main> and update document.title (handoff §8).
  useEffect(() => {
    mainRef.current?.focus()
    document.title = titleFor(location.pathname, applicationName, ctx.environment)
  }, [location.pathname, applicationName, ctx.environment])

  return (
    <div className={`app-shell${collapsed ? ' app-shell--rail' : ''}`}>
      <a className="skip-link" href="#content">
        Skip to content
      </a>

      <Sidebar
        ctx={ctx}
        applicationName={applicationName}
        open={sidebarOpen}
        collapsed={collapsed}
        onToggleCollapse={toggleCollapse}
        onNavigate={() => setSidebarOpen(false)}
      />

      {sidebarOpen && (
        <div
          className="app-shell__scrim app-shell__scrim--open"
          onClick={() => setSidebarOpen(false)}
          aria-hidden="true"
        />
      )}

      <ContextBar
        ctx={ctx}
        applicationName={applicationName}
        onOpenSidebar={() => setSidebarOpen(true)}
        onOpenActivity={() => setDrawerOpen(true)}
      />

      <div className="content">
        {!online && (
          <PageBanner variant="offline">
            You&rsquo;re offline. Showing the last data loaded; mutating actions are disabled.
          </PageBanner>
        )}
        <main id="content" tabIndex={-1} ref={mainRef} style={{ outline: 'none' }}>
          <Outlet />
        </main>
      </div>

      <ActivityDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
      <ToastRegion />
    </div>
  )
}
