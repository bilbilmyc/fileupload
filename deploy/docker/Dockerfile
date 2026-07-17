# fileupload Docker 镜像 — 本地完整构建
# 前端依赖使用 lockfile 安装，确保本地与 CI 构建可复现。

FROM node:20-alpine AS web-builder
WORKDIR /web
COPY sdk/js/package.json sdk/js/package-lock.json /sdk/js/
COPY sdk/js/dist /sdk/js/dist
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25-alpine AS go-builder
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -h /var/lib/fileupload fileupload \
    && mkdir -p /var/lib/fileupload/data /var/lib/fileupload/tmp \
    && chown -R fileupload:fileupload /var/lib/fileupload
COPY --from=go-builder /out/server /usr/local/bin/fileupload-server
COPY fileupload.yaml.example /etc/fileupload/fileupload.yaml
USER fileupload
WORKDIR /var/lib/fileupload
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1
ENTRYPOINT ["/usr/local/bin/fileupload-server"]
