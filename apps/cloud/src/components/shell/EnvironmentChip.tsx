import { ENVIRONMENT_LABELS, type Environment } from '../../routes/builders'

/**
 * Single representation of environment (screens/shell §4).
 * Encoded by text + glyph shape + border style using NEUTRAL tokens only —
 * environment is not a status and never reuses status colors.
 */
const GLYPHS: Record<Environment, string> = {
  production: '■',
  staging: '◧',
  preview: '□',
}

const ABBREVIATIONS: Record<Environment, string> = {
  production: 'PROD',
  staging: 'STG',
  preview: 'PREV',
}

interface EnvironmentChipProps {
  environment: Environment
  size?: 'sm' | 'md'
  abbreviated?: boolean
  /** Environment has no deployment for this application (screens/shell §4.1). */
  notDeployed?: boolean
  locked?: boolean
  ephemeral?: boolean
}

export function EnvironmentChip({
  environment,
  size = 'md',
  abbreviated = false,
  notDeployed = false,
  locked = false,
  ephemeral,
}: EnvironmentChipProps) {
  const full = ENVIRONMENT_LABELS[environment]
  const label = abbreviated ? ABBREVIATIONS[environment] : full

  return (
    <span
      className={`env-chip env-chip--${environment} env-chip--${size}`}
      title={full}
      aria-label={`Environment: ${full}${notDeployed ? ' (not deployed)' : ''}${locked ? ' (locked)' : ''}`}
    >
      <span className="env-chip__glyph" aria-hidden="true">
        {GLYPHS[environment]}
      </span>
      <span>{label}</span>
      {environment === 'preview' && ephemeral && <span className="env-chip__suffix">Ephemeral</span>}
      {notDeployed && <span className="env-chip__suffix">· not deployed</span>}
    </span>
  )
}
