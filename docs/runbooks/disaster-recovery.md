# Runbook: disaster recovery

## Objectives and ownership

Initial recovery objectives are RPO 15 minutes for PostgreSQL and RTO 60 minutes for the public route-search capability, subject to validation by measured restore exercises. Redis contains disposable coordination/idempotency/rate-limit state and is rebuilt rather than restored. GitOps, signed registry artifacts and the secret manager are sources for stateless recovery.

The incident commander authorizes disaster recovery. Database, platform, application, security/privacy and communications leads own their respective steps. A restore exercise is required at least quarterly and after material database/cluster/backup changes.

## Preparation

- PostgreSQL uses encrypted automated snapshots plus point-in-time WAL recovery in a separate failure domain; backup encryption keys are independently controlled.
- Backup jobs alert on age/failure and regularly run checksum/restore verification. Retention is 35 days unless a shorter approved policy applies.
- Release image/chart digests, SBOMs, signatures and configuration live outside the cluster. Secrets are versioned in the secret manager, never in backup manifests.
- DNS/TLS/identity/provider dependencies and authorized break-glass contacts are documented in the private operations system.

## Recovery procedure

1. Declare severity, stop deployments and determine failure boundary: application, database, region/cluster, registry, identity or provider. Preserve audit evidence without copying route data.
2. If writes risk corruption, stop edge admission or scale writers down through the controlled platform path. Record the cutoff timestamp used for RPO.
3. Provision a clean cluster/namespace and platform dependencies from reviewed infrastructure/GitOps definitions. Verify NetworkPolicy-enforcing CNI, Pod Security admission, ingress TLS, monitoring CRDs and workload identity.
4. Restore PostgreSQL to a new instance at the latest consistent point before corruption. Validate backup checksums, schema migration version, row/index consistency and deletion ledger before application access. Never overwrite the failed database in place.
5. Provision empty Redis. Recreate the runtime Secret from approved secret-manager versions; rotate credentials if compromise is possible.
6. Verify signatures/provenance and deploy the last known-safe chart and exact image digests. Allow the migration Job only after database owner confirms compatibility.
7. Keep public ingress closed while running health checks and synthetic FASTEST/GREENEST, ownership/auth, hard constraints, SSE/cancellation, provider budget and telemetry checks.
8. Open traffic gradually, watch availability/latency, database saturation/errors, provider request/cost ratio, circuit state, degraded/confidence ratios and security signals.
9. Move DNS only after TLS and readiness succeed. Retain the failed environment isolated for investigation according to incident/privacy policy.

## Data validation and closure

Reconcile the last committed audit/search metadata against the recovery timestamp and quantify loss without exposing coordinates. Replay deletion requests that occurred after the restored snapshot before serving affected accounts. Confirm no raw provider payload/cache was restored contrary to ADR-005.

Close only after objectives and user impact are measured, backup gaps are fixed, credentials are safe, monitoring is complete and a post-incident review is scheduled. Update the measured RPO/RTO; aspirational targets must not be reported as achieved tests.
