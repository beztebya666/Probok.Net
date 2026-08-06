import { z } from "zod";

export const RoutingModeSchema = z.enum(["FASTEST", "BALANCED", "GREENEST", "STRICT_GREEN"]);
export type RoutingMode = z.infer<typeof RoutingModeSchema>;

export const GeoPointSchema = z.object({
  latitude: z.number().finite().min(-90).max(90),
  longitude: z.number().finite().min(-180).max(180),
});
export type GeoPoint = z.infer<typeof GeoPointSchema>;

export const GeoSuggestionSchema = z.object({
  id: z.string().min(1),
  label: z.string().min(1),
  subtitle: z.string().optional(),
  point: GeoPointSchema,
});
export type GeoSuggestion = z.infer<typeof GeoSuggestionSchema>;

export const GeosuggestResponseSchema = z.object({
  suggestions: z.array(GeoSuggestionSchema).max(20),
});

export const CongestionClassSchema = z.enum(["GREEN", "YELLOW", "ORANGE", "RED", "UNKNOWN"]);
export type CongestionClass = z.infer<typeof CongestionClassSchema>;
export const TrafficDataTypeSchema = z.enum(["REALTIME", "FORECAST", "BASELINE", "SYNTHETIC", "UNKNOWN"]);
export type TrafficDataType = z.infer<typeof TrafficDataTypeSchema>;

export type Confidence = {
  level: "LOW" | "MEDIUM" | "HIGH";
  score: number;
  reasons: string[];
};

const ConfidenceObjectSchema = z.object({
  level: z.enum(["LOW", "MEDIUM", "HIGH"]).optional(),
  score: z.number().finite().min(0).max(1),
  reasons: z.array(z.string()).optional(),
});

function confidenceLevel(score: number): Confidence["level"] {
  if (score >= 0.8) return "HIGH";
  if (score >= 0.55) return "MEDIUM";
  return "LOW";
}

export const ConfidenceSchema = z
  .union([z.number().finite().min(0).max(1), z.enum(["LOW", "MEDIUM", "HIGH"]), ConfidenceObjectSchema])
  .transform<Confidence>((value) => {
    if (typeof value === "number") return { score: value, level: confidenceLevel(value), reasons: [] };
    if (typeof value === "string") {
      const score = value === "HIGH" ? 0.9 : value === "MEDIUM" ? 0.67 : 0.35;
      return { score, level: value, reasons: [] };
    }
    return {
      score: value.score,
      level: value.level ?? confidenceLevel(value.score),
      reasons: value.reasons ?? [],
    };
  });

export const RouteSegmentSchema = z.object({
  segmentId: z.string().min(1),
  geometry: z.array(GeoPointSchema).min(2).max(4096),
  distanceMeters: z.number().finite().nonnegative(),
  liveDurationSeconds: z.number().finite().nonnegative(),
  baselineDurationSeconds: z.number().finite().nonnegative(),
  trafficRatio: z.number().finite().nonnegative(),
  congestionClass: CongestionClassSchema,
  confidence: ConfidenceSchema,
  geometrySimilarity: z.number().finite().min(0).max(1).optional(),
  source: z.string().min(1),
});
export type RouteSegment = z.infer<typeof RouteSegmentSchema>;

const NumericMetricsSchema = z
  .object({
    greenDurationSeconds: z.number().finite().nonnegative().optional(),
    yellowDurationSeconds: z.number().finite().nonnegative().optional(),
    orangeDurationSeconds: z.number().finite().nonnegative().optional(),
    redDurationSeconds: z.number().finite().nonnegative().optional(),
    unknownDurationSeconds: z.number().finite().nonnegative().optional(),
    stopAndGoScore: z.number().finite().nonnegative().optional(),
    routeStabilityScore: z.number().finite().nonnegative().optional(),
  })
  .catchall(z.unknown());

export const RouteCandidateSchema = z.object({
  candidateId: z.string().min(1),
  provider: z.string().min(1),
  providerRouteReference: z.string().optional(),
	trafficDataType: TrafficDataTypeSchema,
  geometry: z.array(GeoPointSchema).min(2).max(4096),
  distanceMeters: z.number().finite().positive(),
  liveDurationSeconds: z.number().finite().positive(),
  baselineDurationSeconds: z.number().finite().nonnegative(),
  trafficDelaySeconds: z.number().finite().nonnegative(),
  segments: z.array(RouteSegmentSchema).min(1).max(2000),
  blocked: z.boolean(),
  tolls: z.boolean(),
  unpaved: z.boolean().optional().default(false),
  confidence: ConfidenceSchema,
  score: z.number().finite(),
  reasonCodes: z.array(z.string()),
	explanation: z.string(),
  metrics: NumericMetricsSchema.optional().default({}),
  generatedBy: z.string().min(1),
  providerRequestCount: z.number().int().nonnegative().optional().default(0),
});
export type RouteCandidate = z.infer<typeof RouteCandidateSchema>;

export const SearchStatusSchema = z.enum([
  "ACCEPTED",
  "QUEUED",
  "SEARCHING",
  "INITIAL_READY",
  "PARTIAL",
  "COMPLETED",
  "DEGRADED",
  "FAILED",
  "CANCELLED",
  "EXPIRED",
]);
export type SearchStatus = z.infer<typeof SearchStatusSchema>;

export const WarningSchema = z
  .union([
    z.string().min(1),
    z.object({ code: z.string().min(1), message: z.string().optional() }).passthrough(),
  ])
  .transform((warning) =>
    typeof warning === "string" ? { code: warning, message: undefined } : { code: warning.code, message: warning.message },
  );
export type RouteWarning = z.infer<typeof WarningSchema>;

export const RouteSearchResultSchema = z.object({
  searchId: z.string().min(1),
  requestId: z.string().optional(),
  status: SearchStatusSchema,
  selectedRoute: RouteCandidateSchema.nullish().transform((value) => value ?? null),
  // Older orchestrator instances serialized an empty Go slice as null. Keep
  // the browser tolerant during rolling upgrades while the canonical API
  // response remains an array.
  alternatives: z
    .array(RouteCandidateSchema)
    .max(50)
    .nullish()
    .transform((value) => value ?? []),
  bestEffortRoutes: z
    .array(RouteCandidateSchema)
    .max(3)
    .nullish()
    .transform((value) => value ?? []),
  // Proof-of-search ranking by measured green share. Older orchestrators do not
  // send it, so an absent field is an empty ranking rather than a parse error.
  greenTopRoutes: z
    .array(RouteCandidateSchema)
    .max(3)
    .nullish()
    .transform((value) => value ?? []),
  fastestReferenceRoute: RouteCandidateSchema.nullish().transform((value) => value ?? null),
  constraints: z.record(z.string(), z.unknown()).default({}),
  providerUsage: z.record(z.string(), z.unknown()).default({}),
  warnings: z
    .array(WarningSchema)
    .nullish()
    .transform((value) => value ?? []),
  scoringPolicyVersion: z.string().optional(),
  generatedAt: z.iso.datetime({ offset: true }),
  expiresAt: z.iso.datetime({ offset: true }),
});
export type RouteSearchResult = z.infer<typeof RouteSearchResultSchema>;

export const RouteSearchRequestSchema = z.object({
  requestId: z.uuid(),
  origin: GeoPointSchema,
  destination: GeoPointSchema,
  waypoints: z.array(GeoPointSchema).max(16),
  departureTime: z.iso.datetime({ offset: true }),
  routingMode: RoutingModeSchema,
  maxExtraDistanceMeters: z.number().int().min(0).max(150_000),
  maxExtraDistancePercent: z.number().min(0).max(300),
  maxExtraTimeSeconds: z.number().int().min(0).max(18_000),
  avoidTolls: z.boolean(),
  avoidUnpaved: z.boolean(),
  strictness: z.number().min(0).max(1),
  maxProviderRequests: z.number().int().min(1).max(20),
  searchDeadlineMs: z.number().int().min(500).max(30_000),
});
export type RouteSearchRequest = z.infer<typeof RouteSearchRequestSchema>;

export const SearchEventSchema = z.object({
  eventId: z.union([z.string().min(1), z.number().int().nonnegative()]).transform(String),
  searchId: z.string().min(1),
  type: z.enum([
    "SEARCH_ACCEPTED",
    "PROVIDER_REQUEST_STARTED",
    "QUEUED",
    "INITIAL_CANDIDATES_READY",
    "CANDIDATE_EVALUATED",
    "DETOUR_SEARCH_STARTED",
    "BETTER_ROUTE_FOUND",
    "SEARCH_COMPLETED",
    "SEARCH_DEGRADED",
    "SEARCH_FAILED",
    "BASELINE_EVALUATION_STARTED",
    "CANDIDATE_DISCOVERED",
    "SCORING_STARTED",
    "PROGRESS",
    "RESULT_UPDATED",
    "COMPLETED",
    "DEGRADED",
    "FAILED",
    "CANCELLED",
    "HEARTBEAT",
  ]),
  timestamp: z.iso.datetime({ offset: true }),
  message: z.string().optional(),
  progress: z.number().finite().min(0).max(100).optional(),
  metadata: z.record(z.string(), z.unknown()).optional(),
  candidate: RouteCandidateSchema.optional(),
  result: RouteSearchResultSchema.optional(),
});
export type SearchEvent = z.infer<typeof SearchEventSchema>;

export const SessionSchema = z.object({
  userId: z.string().optional(),
  displayName: z.string().optional(),
  roles: z.array(z.string()),
  authenticated: z.boolean(),
});
export type Session = z.infer<typeof SessionSchema>;

export const APIIntegrationSchema = z.object({
  id: z.string().min(1).max(128),
  provider: z.string().min(1).max(128),
  product: z.string().min(1).max(128),
  apiVersion: z.string().min(1).max(128),
  capability: z.enum(["routing", "address-search"]),
  role: z.enum(["primary", "fallback"]),
  state: z.enum(["active", "standby"]),
}).strict();
export type APIIntegration = z.infer<typeof APIIntegrationSchema>;

const CanonicalAdminOverviewSchema = z
  .object({
    provider: z.string(),
    status: z.string(),
    circuitBreaker: z.string(),
    requestCount: z.number().nonnegative(),
    estimatedCost: z.number().nonnegative(),
    scoringPolicy: z.string(),
    degradedPercent: z.number().min(0).max(100),
    lowConfidencePercent: z.number().min(0).max(100),
    searchBudgetExhaustion: z.number().nonnegative(),
    featureFlags: z.record(z.string(), z.boolean()),
    apiIntegrations: z.array(APIIntegrationSchema).max(8).optional().default([]),
  })
  .passthrough();

const NestedAdminOverviewSchema = z.object({
  provider: z.object({
    name: z.string(),
    status: z.string(),
    requestCount: z.number().nonnegative().optional().default(0),
    estimatedBillableUnits: z.number().nonnegative().optional().default(0),
    estimatedCost: z.number().nonnegative().optional(),
    circuitBreaker: z.string().optional().default("UNKNOWN"),
  }).passthrough(),
  scoringPolicy: z.record(z.string(), z.unknown()),
  searches: z.object({
    total: z.number().nonnegative().optional().default(0),
    degradedPercent: z.number().min(0).max(100).optional().default(0),
    lowConfidenceCount: z.number().nonnegative().optional().default(0),
    searchBudgetExhaustion: z.number().nonnegative().optional().default(0),
  }).passthrough(),
  featureFlags: z.record(z.string(), z.boolean()),
  apiIntegrations: z.array(APIIntegrationSchema).max(8).optional().default([]),
}).passthrough();

export const AdminOverviewSchema = z.preprocess((input) => {
  const canonical = CanonicalAdminOverviewSchema.safeParse(input);
  if (canonical.success) return canonical.data;
  const nested = NestedAdminOverviewSchema.safeParse(input);
  if (!nested.success) return input;
  const value = nested.data;
  const policyVersion = value.scoringPolicy.policyVersion;
  return {
    provider: value.provider.name,
    status: value.provider.status,
    circuitBreaker: value.provider.circuitBreaker,
    requestCount: value.provider.requestCount,
    estimatedCost: value.provider.estimatedCost ?? value.provider.estimatedBillableUnits,
    scoringPolicy: typeof policyVersion === "string" ? policyVersion : "unknown",
    degradedPercent: value.searches.degradedPercent,
    lowConfidencePercent: value.searches.total > 0 ? (value.searches.lowConfidenceCount / value.searches.total) * 100 : 0,
    searchBudgetExhaustion: value.searches.searchBudgetExhaustion,
    featureFlags: value.featureFlags,
    apiIntegrations: value.apiIntegrations,
  };
}, CanonicalAdminOverviewSchema);
export type AdminOverview = z.infer<typeof AdminOverviewSchema>;

export const ApiErrorSchema = z
  .object({
    code: z.string().optional(),
    message: z.string().optional(),
    type: z.string().optional(),
    title: z.string().optional(),
    detail: z.string().optional(),
    correlationId: z.string().optional(),
    requestId: z.string().optional(),
    details: z.unknown().optional(),
  })
  .passthrough();

export const terminalStatuses = new Set<SearchStatus>(["COMPLETED", "DEGRADED", "FAILED", "CANCELLED", "EXPIRED"]);
