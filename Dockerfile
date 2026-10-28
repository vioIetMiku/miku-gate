# syntax=docker/dockerfile:1
ARG MIKU_BINARY_SOURCE=prebuilt

FROM debian:bookworm-slim AS runtime
ENV DEBIAN_FRONTEND=noninteractive PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       bash ca-certificates certbot curl iproute2 iptables iputils-ping \
       openvpn openssl procps python3 tar tini util-linux \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /usr/local/lib/miku
COPY scripts/common.py scripts/quick.py CORE_ASSETS.json LICENSE FLAG_ICONS_LICENSE ./
COPY docker/container.py ./container.py
COPY docker/miku.sh /usr/local/bin/miku
COPY docker/entrypoint.sh /usr/local/bin/miku-entrypoint
RUN chmod 600 CORE_ASSETS.json \
    && chmod 755 /usr/local/bin/miku /usr/local/bin/miku-entrypoint

# Release ZIP/tar bundles already contain both architectures; no Go is needed.
FROM runtime AS prebuilt
ARG TARGETARCH
COPY bin/miku-linux-* /tmp/miku-bin/
COPY BUILD.json /tmp/BUILD.json
RUN python3 -B container.py select-binary /tmp/miku-bin /tmp/BUILD.json "$TARGETARCH" /out/miku

# BuildKit skips this stage in prebuilt mode. A plain Git checkout can select it.
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS source
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOTELEMETRY=off
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY src ./src
COPY VERSION ./
RUN test "$TARGETOS" = linux \
    && case "$TARGETARCH" in amd64|arm64) ;; *) exit 1 ;; esac \
    && GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -buildvcs=false -trimpath \
       -ldflags "-s -w -X main.version=$(cat VERSION)" -o /out/miku ./src

FROM ${MIKU_BINARY_SOURCE} AS selected

FROM runtime AS final
COPY --from=selected /out/miku ./miku
# Download and verify the pinned official cores while building the image.
# They are copied into the persistent data directory only when absent.
RUN python3 -B container.py seed-cores
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=10s --start-period=45s --retries=3 \
    CMD ["python3", "-B", "/usr/local/lib/miku/container.py", "healthcheck"]
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/miku-entrypoint"]
CMD ["serve"]
