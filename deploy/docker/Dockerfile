# fileupload 服务端 Docker 镜像
# 多阶段构建：build → release

# ---- 构建阶段 ----
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT" \
    -o /fileupload-server ./cmd/server

# ---- 运行阶段 ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -h /var/lib/fileupload fileupload

# 从构建阶段拷贝二进制
COPY --from=builder /fileupload-server /usr/local/bin/fileupload-server

# 数据目录
RUN mkdir -p /var/lib/fileupload/data /var/lib/fileupload/tmp && \
    chown -R fileupload:fileupload /var/lib/fileupload

# 默认配置文件
COPY fileupload.yaml /etc/fileupload/fileupload.yaml

USER fileupload
WORKDIR /var/lib/fileupload

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/fileupload-server"]
