# syntax=docker/dockerfile:1.7

# Docker Official Image, exact Go patch and multi-platform index digest.
# Build on the host architecture and cross-compile: running the Go toolchain
# under QEMU is slow and needlessly fragile.
FROM --platform=$BUILDPLATFORM golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452 AS build
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY internal ./internal
COPY services/provider-yandex ./services/provider-yandex

RUN CGO_ENABLED=0 GOOS=linux go build \
    -buildvcs=false \
    -trimpath \
    -ldflags="-s -w -buildid=" \
    -o /out/provider-yandex \
    ./services/provider-yandex

FROM scratch

LABEL org.opencontainers.image.title="GreenRoute provider-yandex" \
      org.opencontainers.image.description="Provider-neutral official Yandex routing and geocoding adapter"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/provider-yandex /app/provider-yandex

USER 65532:65532
EXPOSE 8082
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/app/provider-yandex", "healthcheck", "--url", "http://127.0.0.1:8082/health/ready"]
ENTRYPOINT ["/app/provider-yandex"]
