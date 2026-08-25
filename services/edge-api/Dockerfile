# syntax=docker/dockerfile:1.14
# Build on the host architecture and cross-compile: running the Go toolchain
# under QEMU is slow and needlessly fragile.
FROM --platform=$BUILDPLATFORM golang:1.26.5-bookworm@sha256:6c5605ab3a9a9fb3c4eafe5b3d63cdbf3881caf113262b67862547b54a9db599 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY services/edge-api ./services/edge-api
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/edge-api ./services/edge-api

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7
COPY --from=build /out/edge-api /app/edge-api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/edge-api"]
