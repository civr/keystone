# syntax=docker/dockerfile:1
# The SPA build is platform independent: run it natively on the build host.
FROM --platform=$BUILDPLATFORM node:25-alpine AS web
# Must equal the value web.Dockerfile is built with, so the embedded shell
# references exactly the assets keystone-web serves.
ARG KEYSTONE_VERSION=dev
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN KEYSTONE_VERSION="$KEYSTONE_VERSION" npm run build

# Cross-compile on the build host instead of emulating the target.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# The Go binary embeds the SPA shell: replace the placeholder with the real build.
COPY --from=web /src/web/build/200.html internal/httpapi/shell/200.html
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/keystone-core ./cmd/keystone-core

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/keystone-core /usr/local/bin/keystone-core
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/keystone-core"]
CMD ["serve"]
