# syntax=docker/dockerfile:1.7

# Docker Official Image, exact Go patch and multi-platform index digest.
FROM golang:1.26.5-bookworm@sha256:6c5605ab3a9a9fb3c4eafe5b3d63cdbf3881caf113262b67862547b54a9db599 AS build

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
