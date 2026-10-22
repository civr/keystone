# syntax=docker/dockerfile:1
# Cross-compile on the build host instead of emulating the target.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/keystone-gate ./cmd/keystone-gate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/keystone-gate /usr/local/bin/keystone-gate
USER 65532:65532
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/keystone-gate"]
CMD ["serve"]
