# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.5

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build

RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY schema ./schema

ARG TARGETOS=linux
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    case "$TARGETARCH/$TARGETVARIANT" in \
      arm/v*) export GOARM="${TARGETVARIANT#v}" ;; \
    esac && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -buildvcs=false -trimpath \
      -ldflags "-s -w -X main.version=$VERSION" \
      -o /out/gbbconnect ./cmd/gbbconnect && \
    mkdir -p /out/data /out/tmp && \
    chown -R 65532:65532 /out/data /out/tmp

FROM scratch

ARG VERSION=dev
LABEL org.opencontainers.image.title="gbbconnect-go" \
      org.opencontainers.image.description="MQTT-to-Modbus bridge for GbbOptimizer" \
      org.opencontainers.image.version="$VERSION"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/gbbconnect /usr/local/bin/gbbconnect
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/tmp /tmp

ENV HOME=/tmp
USER 65532:65532
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/gbbconnect"]
CMD ["run", "--config", "/config/gbbconnect.yaml"]
