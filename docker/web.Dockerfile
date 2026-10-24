# syntax=docker/dockerfile:1
# The SPA build is platform independent: run it natively on the build host.
FROM --platform=$BUILDPLATFORM node:25-alpine AS web
# Must equal the value core.Dockerfile is built with, so the shell embedded in
# core references exactly the assets served from /srv here.
ARG KEYSTONE_VERSION=dev
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN KEYSTONE_VERSION="$KEYSTONE_VERSION" npm run build

FROM caddy:2-alpine
COPY --from=web /src/web/build /srv
COPY deploy/Caddyfile /etc/caddy/Caddyfile
EXPOSE 80
