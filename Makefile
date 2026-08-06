SHELL := /bin/sh
.DEFAULT_GOAL := help

COMPOSE ?= docker compose
HELM ?= helm
KUBECONFORM ?= kubeconform
K6 ?= k6
CHART := deploy/helm/greenroute
NAMESPACE ?= greenroute
RELEASE ?= greenroute
K6_BASE_URL ?= http://localhost:8080

.PHONY: help bootstrap fmt fmt-check lint test test-unit test-contract test-platform test-integration test-frontend test-e2e build compose-up compose-observability compose-down compose-logs migrate helm-lint helm-template helm-call-graph kubeconform load-test load-failures security-scan sbom validate clean

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; printf "GreenRoute targets:\n"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-24s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

bootstrap: ## Install locked backend and frontend dependencies
	go mod download
	npm --prefix apps/web ci

fmt: ## Format Go, frontend, YAML and Markdown where supported
	gofmt -w $$(find . -type f -name '*.go' -not -path './.git/*')

fmt-check: ## Fail when committed source is not formatted
	@files=$$(gofmt -l $$(find . -type f -name '*.go' -not -path './.git/*')); test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

lint: ## Run backend, frontend and configuration linters
	golangci-lint run ./...
	npm --prefix apps/web run lint
	sh ./tools/validate-config.sh

test: test-unit test-contract test-platform test-frontend ## Run deterministic tests

test-unit: ## Run Go tests with race detector
	sh ./tools/go-test.sh unit

test-contract: ## Run provider/API contract tests
	sh ./tools/go-test.sh contract

test-platform: ## Test deployment identity and platform policy helpers
	python3 -B -m unittest tools/test_github_oidc_exec_credential.py tools/test_helm_values_schema.py

test-integration: ## Run integration tests against the Compose stack
	$(COMPOSE) up -d --build --wait postgres redis provider-yandex routing-orchestrator edge-api
	sh ./tools/integration-smoke.sh
	sh ./tools/go-test.sh integration

test-frontend: ## Run frontend unit tests
	npm --prefix apps/web test

test-e2e: ## Run Playwright against the local full stack
	npm --prefix apps/web run test:e2e

build: ## Build all binaries, web app and container images
	sh ./tools/go-build.sh
	npm --prefix apps/web run build
	$(COMPOSE) build edge-api routing-orchestrator provider-yandex web

compose-up: ## Start the local product stack
	$(COMPOSE) up -d --build --wait postgres redis provider-yandex routing-orchestrator edge-api web

compose-observability: ## Start product and local observability stack
	$(COMPOSE) --profile observability up -d --build --wait

compose-down: ## Stop the local stack without deleting data
	$(COMPOSE) --profile observability down

compose-logs: ## Follow service logs
	$(COMPOSE) --profile observability logs -f --tail=200

migrate: ## Apply database migrations through the release binary
	$(COMPOSE) run --rm edge-api migrate up

helm-lint: ## Lint the Helm chart for every supported environment
	$(HELM) lint $(CHART) -f $(CHART)/values-dev.yaml --strict
	$(HELM) lint $(CHART) -f $(CHART)/values-stage.yaml --strict
	$(HELM) lint $(CHART) -f $(CHART)/values-prod.yaml --strict

helm-template: ## Render production manifests locally
	$(HELM) template $(RELEASE) $(CHART) --namespace $(NAMESPACE) -f $(CHART)/values-prod.yaml > .greenroute-rendered.yaml

helm-call-graph: helm-template ## Assert Services, URLs and NetworkPolicies permit the application call graph
	python3 tools/assert-rendered-call-graph.py .greenroute-rendered.yaml

kubeconform: helm-call-graph ## Validate rendered Kubernetes resources
	$(KUBECONFORM) -strict -summary -ignore-missing-schemas .greenroute-rendered.yaml

load-test: ## Run the normal and peak k6 scenarios
	$(K6) run -e BASE_URL=$(K6_BASE_URL) tests/load/route-search.js

load-failures: ## Run provider failure and retry-storm checks
	$(K6) run -e BASE_URL=$(K6_BASE_URL) tests/load/provider-failures.js

security-scan: ## Scan source, filesystem dependencies and local images
	gitleaks detect --source . --no-banner
	trivy fs --exit-code 1 --severity HIGH,CRITICAL --ignore-unfixed .
	@for image in edge-api routing-orchestrator provider-yandex web; do trivy image --exit-code 1 --severity HIGH,CRITICAL --ignore-unfixed greenroute/$$image:0.1.0-local; done

sbom: ## Generate CycloneDX SBOMs for local images
	@mkdir -p dist/sbom
	@for image in edge-api routing-orchestrator provider-yandex web; do syft greenroute/$$image:0.1.0-local -o cyclonedx-json=dist/sbom/$$image.cdx.json; done

validate: fmt-check lint test helm-lint kubeconform ## Run the local CI quality gate

clean: ## Remove generated validation output only
	rm -f .greenroute-rendered.yaml
