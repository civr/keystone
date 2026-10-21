# syntax=docker/dockerfile:1
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The Go binary embeds the SPA shell: replace the placeholder with the real build.
COPY --from=web /src/web/build/200.html internal/httpapi/shell/200.html
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/keystone-core ./cmd/keystone-core

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/keystone-core /usr/local/bin/keystone-core
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/keystone-core"]
CMD ["serve"]
