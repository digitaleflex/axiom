import { useState } from 'react'

/**
 * Write-only secret input (handoff/deployment §4, deployment-configuration §5.4).
 *
 * - `type=password`, `autocomplete="off"`, `spellcheck=false`.
 * - Once saved, the value is never revealed, never prefilled, never stored
 *   in browser storage, URLs or logs — the UI only knows `{ isSet: true }`.
 * - Replace re-opens an empty input; the previous value is never echoed.
 */
export function SecretField({
  name,
  isSet,
  required,
  onChange,
  onReplace,
  onRemove,
}: {
  name: string
  isSet: boolean
  required?: boolean
  onChange: (value: string) => void
  onReplace: () => void
  onRemove?: () => void
}) {
  const [draft, setDraft] = useState('')
  const [show, setShow] = useState(false)

  if (isSet) {
    return (
      <div className="secret-field secret-field--set">
        <span className="secret-field__value" aria-label={`${name} is set`}>
          •••••••• set
        </span>
        <button type="button" className="btn btn--ghost btn--sm" onClick={onReplace}>
          Replace
        </button>
        {onRemove && (
          <button type="button" className="btn btn--ghost btn--sm" onClick={onRemove}>
            Remove
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="secret-field">
      <input
        className="field__input"
        type={show ? 'text' : 'password'}
        value={draft}
        placeholder={required ? 'Required' : 'Optional'}
        autoComplete="off"
        spellCheck={false}
        aria-label={name}
        onChange={(event) => {
          setDraft(event.target.value)
          onChange(event.target.value)
        }}
      />
      <button type="button" className="btn btn--ghost btn--sm" onClick={() => setShow((v) => !v)} aria-pressed={show}>
        {show ? 'Hide' : 'Show'}
      </button>
    </div>
  )
}
