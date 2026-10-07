/**
 * Idempotency keys (api-contract §20, handoff/deployment §6).
 *
 * The key is deterministic per plan: `deploy:<planId>`. Double-clicks,
 * retries and page refreshes reuse the same key, so one plan can never
 * create two deployments. A key cannot silently create a second deployment;
 * conflicting reuse returns 409 CONFLICT from the Engine.
 */
export function idempotencyKeyForPlan(planId: string): string {
  return `deploy:${planId}`
}
