# syntax=docker/dockerfile:1.14
FROM golang:1.26.5-bookworm@sha256:6c5605ab3a9a9fb3c4eafe5b3d63cdbf3881caf113262b67862547b54a9db599 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY services/routing-orchestrator ./services/routing-orchestrator
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/routing-orchestrator ./services/routing-orchestrator

FROM gcr.io/distroless/static-debian13:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6
COPY --from=build /out/routing-orchestrator /app/routing-orchestrator
USER nonroot:nonroot
EXPOSE 8081
ENTRYPOINT ["/app/routing-orchestrator"]
