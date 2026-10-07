import { useToast } from './ToastContext'

/** Bottom-right toast stack (shell §5.2). Success is polite, failure is alert. */
export function ToastRegion() {
  const { toasts, dismiss } = useToast()

  return (
    <div className="toast-region" aria-live="polite">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={`toast toast--${toast.variant}`}
          role={toast.variant === 'failure' ? 'alert' : 'status'}
        >
          <div className="toast__content">
            <div className="toast__title">{toast.title}</div>
            {toast.body && <div className="toast__body">{toast.body}</div>}
          </div>
          <button type="button" className="notice__close" aria-label="Dismiss" onClick={() => dismiss(toast.id)}>
            ×
          </button>
        </div>
      ))}
    </div>
  )
}
