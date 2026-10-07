import { InlineNotice } from './system'

interface PlaceholderProps {
  params: Record<string, string | undefined>
  resources: string[]
  note?: string
}

/**
 * Scaffold marker for routes whose real screen lands in #120. It surfaces the
 * route params and the API resources the screen will consume so the wiring is
 * unambiguous.
 */
export function Placeholder({ params, resources, note }: PlaceholderProps) {
  return (
    <div className="stack">
      <InlineNotice variant="info" title="Placeholder screen">
        This area is scaffolded by #119 (shell &amp; auth). The real screen is wired in #120.
      </InlineNotice>

      <section>
        <h2 className="section-title">Route parameters</h2>
        <div className="data-list">
          {Object.entries(params).map(([key, value]) => (
            <div className="data-list__row" key={key}>
              <div className="data-list__main">
                <div className="data-list__title mono">{key}</div>
              </div>
              <span className="mono muted">{value ?? '—'}</span>
            </div>
          ))}
        </div>
      </section>

      <section>
        <h2 className="section-title">API resources this screen will consume</h2>
        <div className="data-list">
          {resources.map((resource) => (
            <div className="data-list__row" key={resource}>
              <span className="mono">{resource}</span>
            </div>
          ))}
        </div>
      </section>

      {note && <p className="muted">{note}</p>}
    </div>
  )
}
