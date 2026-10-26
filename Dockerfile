FROM node:22-bookworm-slim AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27.3-bookworm AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/roadmap ./cmd/roadmap

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* && groupadd -g 10001 roadmap && useradd -r -u 10001 -g roadmap roadmap && mkdir -p /data && chown roadmap:roadmap /data
WORKDIR /app
COPY --from=backend /out/roadmap /app/roadmap
COPY --from=frontend /src/frontend/dist /app/public
ENV ROADMAP_ADDR=:8080 ROADMAP_DB=/data/roadmap.db ROADMAP_STATIC=/app/public ROADMAP_COOKIE_SECURE=true
USER 10001:10001
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/app/roadmap"]
