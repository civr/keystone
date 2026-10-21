# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/keystone-init ./cmd/keystone-init
# Created here so a fresh named volume mounted on /run/keystone inherits
# ownership by uid 65532 (the same uid core runs as).
RUN mkdir -p /out/run/keystone && chown 65532:65532 /out/run/keystone && chmod 0700 /out/run/keystone

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/keystone-init /usr/local/bin/keystone-init
COPY --from=build --chown=65532:65532 /out/run/keystone /run/keystone
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/keystone-init"]
