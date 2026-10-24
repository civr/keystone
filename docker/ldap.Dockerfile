# syntax=docker/dockerfile:1
# Cross-compile on the build host instead of emulating the target.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/keystone-ldap ./cmd/keystone-ldap

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/keystone-ldap /usr/local/bin/keystone-ldap
USER 65532:65532
EXPOSE 636 389
ENTRYPOINT ["/usr/local/bin/keystone-ldap"]
CMD ["serve"]
