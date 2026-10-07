import { Link } from 'react-router-dom'
import { useAuth } from '../../auth/AuthContext'
import { routes } from '../../routes/builders'
import type { RouteContext } from '../../routes/context'
import { EnvironmentChip } from './EnvironmentChip'

interface ContextBarProps {
  ctx: RouteContext
  applicationName?: string
  onOpenSidebar: () => void
  onOpenActivity: () => void
}

/** Top chrome: workspace (display-only), application switcher, environment chip. */
export function ContextBar({ ctx, applicationName, onOpenSidebar, onOpenActivity }: ContextBarProps) {
  const { user, signOut } = useAuth()
  const hasApplication = Boolean(ctx.applicationId)

  return (
    <header className="context-bar">
      <div className="context-bar__left">
        <button
          type="button"
          className="icon-button menu-button"
          aria-label="Open navigation"
          onClick={onOpenSidebar}
        >
          ☰
        </button>

        <div className="context-path">
          {/* Workspace is display-only in V0.1 (navigation §9, D-5). */}
          <div className="workspace" title="Workspace (display-only in V0.1)">
            <span className="avatar" aria-hidden="true">
              L
            </span>
            <span>Local workspace</span>
          </div>

          {hasApplication && (
            <>
              <span className="context-path__sep" aria-hidden="true">
                /
              </span>
              {/* Application switcher placeholder — wired in #120. */}
              <button
                type="button"
                className="app-switcher"
                aria-haspopup="listbox"
                title="Application switcher (wired in #120)"
              >
                <span className="app-switcher__name">{applicationName ?? ctx.applicationId}</span>
                <span className="app-switcher__caret" aria-hidden="true">
                  ▾
                </span>
              </button>
            </>
          )}

          {hasApplication && ctx.environment && (
            <>
              <span className="context-path__sep" aria-hidden="true">
                /
              </span>
              <EnvironmentChip environment={ctx.environment} />
            </>
          )}
        </div>
      </div>

      <div className="context-bar__right">
        <Link className="btn btn--secondary" to={routes.repositories()}>
          New application
        </Link>
        <button
          type="button"
          className="icon-button"
          aria-label="Activity"
          onClick={onOpenActivity}
        >
          ◔
        </button>
        <button type="button" className="icon-button" aria-label={`Signed in as ${user?.name ?? 'operator'}`}>
          <span className="avatar" aria-hidden="true">
            {(user?.name ?? 'L').slice(0, 1).toUpperCase()}
          </span>
        </button>
        <button type="button" className="btn btn--ghost" onClick={() => void signOut()}>
          Sign out
        </button>
      </div>
    </header>
  )
}
