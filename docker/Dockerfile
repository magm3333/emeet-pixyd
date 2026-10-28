# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.27.1

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src
ENV GOTOOLCHAIN=local GOWORK=off

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.buildVersion=${VERSION}" \
    -o /out/emeet-pixyd .

FROM alpine:3.22 AS runtime-base

ARG INSTALL_DESKTOP_TOOLS=0
ARG VERSION=dev
LABEL org.opencontainers.image.title="EMEET PIXY dual-camera daemon" \
      org.opencontainers.image.description="Linux daemon and web UI for EMEET PIXY and PIXY 2K" \
      org.opencontainers.image.source="https://github.com/magm3333/emeet-pixyd" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version=${VERSION}

RUN apk add --no-cache \
        ca-certificates \
        ffmpeg \
        v4l-utils \
    && if [ "${INSTALL_DESKTOP_TOOLS}" = "1" ]; then \
        apk add --no-cache libnotify wireplumber; \
    fi \
    && addgroup -S -g 65532 emeet \
    && adduser -S -D -H -u 65532 -G emeet -h /var/lib/emeet-pixyd \
        -s /sbin/nologin emeet \
    && mkdir -p /var/lib/emeet-pixyd \
    && chown 65532:65532 /var/lib/emeet-pixyd \
    && chmod 0750 /var/lib/emeet-pixyd

COPY --from=build --chown=65532:65532 /out/emeet-pixyd /usr/local/bin/emeet-pixyd
RUN ln -s /usr/local/bin/emeet-pixyd /usr/local/bin/emeet-pixy

ENV EMEET_PIXYD_STATE_DIR=/var/lib/emeet-pixyd \
    EMEET_PIXYD_WEB_ADDR=0.0.0.0:8090 \
    EMEET_PIXYD_AUTO=off \
    EMEET_PIXYD_DEFAULT_AUDIO=nc

USER 65532:65532

# BuildKit executes this stage for each target architecture. It exercises the
# actual target binaries under native execution or its registered QEMU runner.
FROM runtime-base AS smoke
RUN /usr/local/bin/emeet-pixyd --version >/dev/null \
    && ffmpeg -version >/dev/null 2>&1 \
    && v4l2-ctl --version >/dev/null 2>&1 \
    && if [ "${INSTALL_DESKTOP_TOOLS}" != "1" ] || { command -v wpctl >/dev/null && notify-send --help >/dev/null 2>&1; }; then :; else exit 1; fi \
    && test -w /var/lib/emeet-pixyd

FROM smoke AS runtime
VOLUME ["/var/lib/emeet-pixyd"]
EXPOSE 8090

# The socket is created by the daemon only after it has initialized.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD test -S /var/lib/emeet-pixyd/control.sock

ENTRYPOINT ["/usr/local/bin/emeet-pixyd"]
