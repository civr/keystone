# syntax=docker/dockerfile:1
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM caddy:2-alpine
COPY --from=web /src/web/build /srv
COPY deploy/Caddyfile /etc/caddy/Caddyfile
EXPOSE 80
