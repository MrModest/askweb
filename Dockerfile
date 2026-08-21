# syntax=docker/dockerfile:1

# =============================================================================
# Stage 1: Build
# =============================================================================
FROM --platform=${BUILDPLATFORM} golang:1.26.5-alpine3.23 AS builder

WORKDIR /src

# Copy dependency files first (layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source files explicitly (no COPY . .)
COPY cmd ./cmd
COPY internal ./internal

# CGO off, so the binary is static and carries no libc dependency into the
# runtime stage. Go cross-compiles, so one builder serves every target
# platform; TARGETOS/TARGETARCH are set by BuildKit and default to the host.
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/askweb ./cmd/askweb

# =============================================================================
# Stage 2: Runtime
# =============================================================================
FROM alpine:3.23 AS runtime

# OCI labels for traceability
LABEL org.opencontainers.image.title="askweb"
LABEL org.opencontainers.image.description="Whitelisted web-access MCP server"
LABEL org.opencontainers.image.source="https://github.com/MrModest/askweb"
LABEL org.opencontainers.image.url="https://github.com/MrModest/askweb"
LABEL org.opencontainers.image.documentation="https://github.com/MrModest/askweb/blob/main/README.md"

# Root certificate store, for OUTBOUND TLS: askweb verifies the certificates of
# the whitelisted hosts it fetches, and Go reads the roots from disk. Without a
# bundle every fetch fails with "x509: certificate signed by unknown authority"
# while the server itself still looks healthy. Installed explicitly so the
# dependency is stated, rather than COPYed out of the builder stage where it
# would silently stop matching when the builder image changes.
RUN apk add --no-cache ca-certificates

# Default non-root user. The uid is deliberately arbitrary and nothing in the
# image depends on it, on this user existing in /etc/passwd, or on $HOME:
# an operator may override it with any uid:gid (see docker-compose.yml).
RUN addgroup -g 10001 -S askweb && \
    adduser -u 10001 -S -H -D -G askweb askweb

WORKDIR /app

COPY --from=builder /out/askweb /app/askweb

# Configuration comes from the environment, so an operator can override either
# without a custom command. Nothing binds below port 1024, since a non-root
# user cannot. The whitelist path is absolute and inside the data directory, so
# the mount point is unambiguous.
ENV ASKWEB_ADDR=":8080"
ENV ASKWEB_WHITELIST="/app/data/whitelist.json"

# Arbitrary-uid support. An overridden user owns nothing here and belongs to no
# group in this image, so everything it reads must be world-readable and every
# directory it walks world-traversable — otherwise `user: "1003:1002"` cannot
# even exec the binary. Done as a temporary root USER, then dropped back.
#
# The data directory gets the same read treatment and is owned by the default
# user; making it writable by an unknown future uid is NOT attempted, because no
# build-time mode does that short of 0777, which is not a reasonable mode for
# the directory holding the security boundary. Matching the mount's ownership is
# the operator's job (see docker-compose.yml).
USER root
RUN mkdir -p /app/data && \
    chown askweb:askweb /app/data && \
    chmod -R a+r /app && \
    find /app -type d -exec chmod a+rx {} \; && \
    chmod a+rx /app/askweb
USER askweb

EXPOSE 8080

CMD ["/app/askweb"]
