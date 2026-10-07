import { Skeleton } from './Skeleton'

/** Initial shell/auth loading. No full-page spinner (shell §9). */
export function FullPageLoading({ label = 'Loading' }: { label?: string }) {
  return (
    <div className="login" role="status" aria-live="polite">
      <div className="login__card" aria-busy="true">
        <span className="visually-hidden">{label}</span>
        <Skeleton width={120} height={24} />
        <Skeleton width="80%" height={16} />
        <Skeleton width="60%" height={16} />
      </div>
    </div>
  )
}
