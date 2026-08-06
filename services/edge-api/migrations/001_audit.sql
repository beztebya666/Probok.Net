CREATE TABLE IF NOT EXISTS route_search_audit (
    search_id uuid PRIMARY KEY,
    request_id text NOT NULL,
    subject_hash text NOT NULL,
    routing_mode text NOT NULL CHECK (routing_mode IN ('FASTEST','BALANCED','GREENEST','STRICT_GREEN')),
    max_extra_distance_bucket_m bigint NOT NULL CHECK (max_extra_distance_bucket_m >= 0),
    provider_request_budget integer NOT NULL CHECK (provider_request_budget > 0),
    accepted_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS route_search_audit_accepted_at_idx ON route_search_audit (accepted_at DESC);
COMMENT ON TABLE route_search_audit IS 'Privacy-minimized search audit; exact coordinates and provider geometry are intentionally excluded.';
