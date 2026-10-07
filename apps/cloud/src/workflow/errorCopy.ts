/**
 * Error-code → copy mapping (deployment-failure §5).
 *
 * The UI branches on `error.code` only (api-contract §18: the message is
 * not the stable machine contract). Unknown codes render raw — the screen
 * still names the failed step and impact.
 */
export interface ErrorCopy {
  /** One-sentence L1 explanation. */
  explanation: string
  /** Actionable checklist entries; each links to a correction path. */
  checklist: string[]
}

const COPY: Record<string, ErrorCopy> = {
  BUILD_FAILED: {
    explanation: 'The build failed.',
    checklist: ['Check the build command and package manager in the profile.', 'Check build configuration values.'],
  },
  RUNTIME_FAILED: {
    explanation: "The application couldn't start.",
    checklist: [
      'Check the start command, port and required configuration values.',
      'Check server capacity — change the server if it is exhausted.',
    ],
  },
  HEALTH_CHECK_FAILED: {
    explanation: 'The application did not respond to the health check.',
    checklist: ['Check the port and health path.', 'Check the start command.'],
  },
  DEPLOYMENT_NOT_ELIGIBLE: {
    explanation: "The selected server can't run this deployment.",
    checklist: ['Check server readiness and capabilities — change the server.'],
  },
  DEPLOYMENT_INVALID_STATE: {
    explanation: 'This deployment was stopped because its state changed.',
    checklist: ['View the deployment list for the current state.'],
  },
  POLICY_DENIED: {
    explanation: "Axiom's security policy blocked this deployment.",
    checklist: ['Review the error details and contact your workspace owner.'],
  },
  INTERNAL_ERROR: {
    explanation: 'Axiom hit an internal error.',
    checklist: ['Retry the deployment.', 'Quote the request ID when contacting support.'],
  },
}

export function deploymentErrorCopy(code: string | undefined | null): ErrorCopy {
  if (code && COPY[code]) return COPY[code]
  return {
    explanation: 'Axiom stopped this deployment.',
    checklist: ['Open the full logs for details.'],
  }
}

/** Analysis failure codes (api-contract §7). */
export function analysisErrorCopy(code: string | undefined | null): string {
  switch (code) {
    case 'REF_NOT_FOUND':
      return 'The ref could not be resolved to a commit.'
    case 'SOURCE_ACCESS_FAILED':
      return "Axiom couldn't read the repository source."
    case 'SNAPSHOT_REJECTED':
      return 'The source snapshot was rejected (oversized, malformed, unsafe or empty).'
    default:
      return 'The analysis could not complete.'
  }
}
