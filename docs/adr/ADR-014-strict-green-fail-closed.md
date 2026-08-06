# ADR-014: STRICT_GREEN is fail-closed

- Status: accepted
- Date: 2026-08-05
- Policy: `greenroute-scoring-v2.0.0`
- Supersedes the STRICT_GREEN fallback semantics in ADR-004

## Context

`STRICT_GREEN` means an all-green route, not merely the least congested route
available. Returning a red, orange, yellow, or unverified route under this mode
would violate the product contract even if it were the fastest provider route.

## Decision

A candidate is eligible for `STRICT_GREEN` only when all of the following hold:

1. The traffic data type is `REALTIME` or `FORECAST`.
2. Route-level confidence is at least `MEDIUM` and meets the scoring policy's
   minimum confidence score.
3. Meaningful segments exist and their distance and duration exactly cover the
   provider route.
4. Every meaningful segment is classified `GREEN`.
5. Every meaningful segment has at least `MEDIUM` confidence and meets the
   scoring policy's minimum confidence score.
6. The existing geometry, detour, toll, surface, time, and distance hard
   constraints also pass.

If no candidate satisfies the invariant, `selectedRoute` is absent and
`alternatives` is an empty array. `fastestReferenceRoute` may remain in the API
only as explicitly labelled diagnostic/reference data; it is never a strict
route result or a UI selection fallback.

`bestEffortRoutes` is a separate, non-selectable proof-of-search array capped at
three candidates. It contains discovered candidates rejected by at least one
`STRICT_GREEN_*` predicate and carries `BEST_EFFORT_NOT_STRICT_GREEN` plus the
precise rejection codes. Ranking is deterministic: greatest known GREEN
distance/duration coverage, then least red, orange, yellow and unknown coverage,
then confidence, ETA, distance and candidate ID. Initial and enhanced candidates
are deduplicated together before this ranking.

Class-specific codes (`STRICT_GREEN_RED_SEGMENT`,
`STRICT_GREEN_ORANGE_SEGMENT`, `STRICT_GREEN_YELLOW_SEGMENT`, and
`STRICT_GREEN_UNKNOWN_SEGMENT`) accompany the aggregate rejection code so the
client can explain the exact non-green evidence without reconstructing policy.

The orchestrator may continue a bounded enhanced search from a rejected
provider candidate. Every returned route geometry must still be built by the
configured motor-vehicle routing provider. Пробок.Нет does not synthesize yard
access, relax access restrictions, or reverse one-way direction rules.

Per-geometry 2GIS traffic colors are retained as inferred provider evidence.
Known colors receive `MEDIUM` segment confidence and an explicit inference
reason; unknown colors remain `UNKNOWN`. A complete provider-colored route does
not consume a second baseline request because the color already belongs to the
exact route geometry.

## Consequences

- A zero-result response is correct when an all-green route cannot be proven.
- Up to three near-green candidates may still be shown from `bestEffortRoutes`,
  but they must be visually and semantically distinct from route results.
- `UNKNOWN`, yellow, orange, and red can guide detour exploration but cannot be
  exposed as strict alternatives.
- Product copy and analytics must distinguish a diagnostic fastest reference
  from a verified strict-green result.
- Regression tests enforce empty strict results for unverified and congested
  candidates.
