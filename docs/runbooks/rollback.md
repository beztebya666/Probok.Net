# Runbook: application rollback

## Decision

Rollback when a release causes SLO burn, authorization/privacy regression, provider amplification, invalid scoring/constraints, crash loops or migration/runtime incompatibility. For an actively exploitable security issue, contain traffic/credentials first and then deploy the last verified safe digest.

## Procedure

1. Declare the incident/change, freeze concurrent deployments and capture current release revision, four image digests, chart/config revision, migration Job result and alert timeline.
2. Determine whether the schema change is backward compatible. Пробок.Нет migrations must use expand/contract: additive expansion precedes code, destructive contraction occurs only after the rollback window. Never run a down migration against production data without a reviewed restore plan.
3. Select the previous release whose images have valid signatures, provenance, SBOM and vulnerability policy. Do not rebuild an old tag.
4. Revert the GitOps release values to the previous chart/config and exact image digests, preserving current secret versions unless the incident concerns them. Obtain the protected-environment approval.
5. Sync/deploy. Watch migration hook, rollout status, readiness, edge 5xx/latency, provider-call ratio and database errors. Stop if the old binary cannot read the expanded schema.
6. Run synthetic health, FASTEST/GREENEST, hard-constraint, SSE and cancellation checks. Confirm web assets call the matching API contract.
7. Keep the incident open for at least one SLO evaluation window and verify no retry/cost or privacy/security anomaly.

If rollback is unsafe, roll forward with the smallest reviewed fix or disable the affected feature through fail-closed flags. For database corruption follow [disaster-recovery.md](disaster-recovery.md).

Record why automated canary/health gates did not prevent impact, exact before/after digests, schema state, verification evidence and follow-up owner/date.
