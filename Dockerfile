# syntax=docker/dockerfile:1

# Cross-compile on the build host; the final stage has no RUN steps, so
# multi-arch builds need no QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/server /server
# Numeric so Kubernetes can verify runAsNonRoot.
USER 65532:65532
# 8080: Beam ingest (public via the tunnel), 8081: query API (cluster only)
EXPOSE 8080 8081
ENTRYPOINT ["/server"]
