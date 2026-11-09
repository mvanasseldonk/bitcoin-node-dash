# syntax=docker/dockerfile:1

# Build stages run on the build machine's platform and cross-compile,
# so multi-arch images build fast without emulation.

# 1. Frontend
FROM --platform=$BUILDPLATFORM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2. Go binary (static, with the frontend embedded)
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/node-dash ./cmd/node-dash

# 3. Minimal runtime image: no shell, no package manager, runs as non-root
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/node-dash /usr/local/bin/node-dash

ENV NODEDASH_LISTEN=:8080 \
    BITCOIN_DATADIR=/bitcoin

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/node-dash", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/node-dash"]

LABEL org.opencontainers.image.title="bitcoin-node-dash" \
      org.opencontainers.image.description="A minimal, calm dashboard for your Bitcoin Core node" \
      org.opencontainers.image.source="https://github.com/mvanasseldonk/bitcoin-node-dash" \
      org.opencontainers.image.licenses="MIT"
