# syntax=docker/dockerfile:1.14
# Build on the host architecture and cross-compile: running the Go toolchain
# under QEMU is slow and needlessly fragile.
FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY services/routing-orchestrator ./services/routing-orchestrator
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/routing-orchestrator ./services/routing-orchestrator

FROM gcr.io/distroless/static-debian13:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6
COPY --from=build /out/routing-orchestrator /app/routing-orchestrator
USER nonroot:nonroot
EXPOSE 8081
ENTRYPOINT ["/app/routing-orchestrator"]
