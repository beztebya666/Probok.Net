{{- define "greenroute.validateValues" -}}
{{- if ge (int .Values.config.maxConcurrentSSE) (int .Values.config.maxConcurrentEdgeRequests) -}}
{{- fail "config.maxConcurrentSSE must be less than config.maxConcurrentEdgeRequests" -}}
{{- end -}}
{{- if gt (int .Values.autoscaling.minReplicas) (int .Values.autoscaling.maxReplicas) -}}
{{- fail "autoscaling.minReplicas must not exceed autoscaling.maxReplicas" -}}
{{- end -}}
{{- if eq .Values.secrets.keys.redisUrl .Values.secrets.keys.oauth2RedisUrl -}}
{{- fail "OAuth2 Proxy and application Redis must use distinct Secret keys" -}}
{{- end -}}
{{- if eq .Values.secrets.keys.auditHashKey .Values.secrets.keys.abuseHashKey -}}
{{- fail "audit-chain and abuse-pseudonymization keys must be distinct" -}}
{{- end -}}
{{- if eq .Values.secrets.keys.oauth2ClientSecret .Values.secrets.keys.oauth2CookieSecret -}}
{{- fail "OIDC client and session-cookie secrets must be distinct" -}}
{{- end -}}
{{- if eq .Values.secrets.keys.yandexRouterApiKey .Values.secrets.keys.yandexMapsBrowserApiKey -}}
{{- fail "server Router and browser Maps credentials must be distinct" -}}
{{- end -}}
{{- if eq .Values.secrets.keys.dgisApiKey .Values.secrets.keys.twoGisMapGLBrowserApiKey -}}
{{- fail "server 2GIS Routing and browser MapGL credentials must be distinct" -}}
{{- end -}}
{{- if and .Values.config.features.providerDataModificationAllowed (not .Values.config.features.providerDataStorageAllowed) -}}
{{- fail "providerDataModificationAllowed requires providerDataStorageAllowed" -}}
{{- end -}}
{{- if and .Values.config.features.enableProviderCache (not .Values.config.features.providerDataStorageAllowed) -}}
{{- fail "enableProviderCache requires providerDataStorageAllowed" -}}
{{- end -}}
{{- if and (gt (int (index .Values.services "routing-orchestrator").replicas) 1) (not .Values.config.features.providerDataStorageAllowed) -}}
{{- fail "routing-orchestrator cannot use multiple replicas while provider-derived state storage is disabled" -}}
{{- end -}}
{{- if eq .Values.global.environment "prod" -}}
  {{- if .Values.config.features.enableAnonymousUsage -}}
  {{- fail "production cannot enable anonymous usage" -}}
  {{- end -}}
  {{- if not .Values.authProxy.enabled -}}
  {{- fail "production requires authProxy.enabled=true" -}}
  {{- end -}}
  {{- if or (not .Values.ingress.enabled) (not .Values.ingress.tls.enabled) -}}
  {{- fail "production requires TLS ingress" -}}
  {{- end -}}
  {{- if .Values.config.otelInsecure -}}
  {{- fail "production OTLP transport cannot be marked insecure" -}}
  {{- end -}}
{{- end -}}
{{- end -}}
