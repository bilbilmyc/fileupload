# fileupload 构建系统
# 支持多平台编译、Docker 镜像构建、代码检查

APP_NAME    := fileupload
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILT_AT    := $(shell date +%Y-%m-%dT%H:%M:%S%z)
LDFLAGS     := -ldflags="-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(BUILT_AT)"

# 输出目录
OUTPUT_DIR  := build

# 目标平台列表
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# 默认目标
.PHONY: all
all: clean web server cli

# ---- 前端 ----

.PHONY: web
web:
	@echo "▸ 构建前端"
	@cd web && npm install && npm run build

.PHONY: web-dev
web-dev:
	@echo "▸ 启动前端开发服务器"
	@cd web && npm run dev

# ---- 服务端 ----

.PHONY: server
server: web
	@echo "▸ 编译 server ($(shell go env GOOS)/$(shell go env GOARCH))"
	@mkdir -p $(OUTPUT_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(OUTPUT_DIR)/$(APP_NAME)-server ./cmd/server

.PHONY: server-linux-amd64
server-linux-amd64: web
	@echo "▸ 编译 server (linux/amd64)"
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) \
	  -o $(OUTPUT_DIR)/$(APP_NAME)-server-linux-amd64 ./cmd/server

.PHONY: server-linux-arm64
server-linux-arm64: web
	@echo "▸ 编译 server (linux/arm64)"
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) \
	  -o $(OUTPUT_DIR)/$(APP_NAME)-server-linux-arm64 ./cmd/server

.PHONY: server-darwin-amd64
server-darwin-amd64: web
	@echo "▸ 编译 server (darwin/amd64)"
	@mkdir -p $(OUTPUT_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) \
	  -o $(OUTPUT_DIR)/$(APP_NAME)-server-darwin-amd64 ./cmd/server

.PHONY: server-darwin-arm64
server-darwin-arm64: web
	@echo "▸ 编译 server (darwin/arm64)"
	@mkdir -p $(OUTPUT_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) \
	  -o $(OUTPUT_DIR)/$(APP_NAME)-server-darwin-arm64 ./cmd/server

# ---- CLI ----

.PHONY: cli
cli:
	@echo "▸ 编译 cli ($(shell go env GOOS)/$(shell go env GOARCH))"
	@mkdir -p $(OUTPUT_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(OUTPUT_DIR)/$(APP_NAME)-cli ./cmd/fileupload

.PHONY: cli-linux-amd64
cli-linux-amd64:
	@echo "▸ 编译 cli (linux/amd64)"
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) \
	  -o $(OUTPUT_DIR)/$(APP_NAME)-cli-linux-amd64 ./cmd/fileupload

.PHONY: cli-linux-arm64
cli-linux-arm64:
	@echo "▸ 编译 cli (linux/arm64)"
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) \
	  -o $(OUTPUT_DIR)/$(APP_NAME)-cli-linux-arm64 ./cmd/fileupload

.PHONY: cli-darwin-amd64
cli-darwin-amd64:
	@echo "▸ 编译 cli (darwin/amd64)"
	@mkdir -p $(OUTPUT_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) \
	  -o $(OUTPUT_DIR)/$(APP_NAME)-cli-darwin-amd64 ./cmd/fileupload

.PHONY: cli-darwin-arm64
cli-darwin-arm64:
	@echo "▸ 编译 cli (darwin/arm64)"
	@mkdir -p $(OUTPUT_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) \
	  -o $(OUTPUT_DIR)/$(APP_NAME)-cli-darwin-arm64 ./cmd/fileupload

# ---- 全平台编译 ----

.PHONY: release
release: clean web server-linux-amd64 server-linux-arm64 server-darwin-amd64 server-darwin-arm64 \
  cli-linux-amd64 cli-linux-arm64 cli-darwin-amd64 cli-darwin-arm64
	@echo ""
	@echo "✓ 全平台编译完成"
	@ls -lh $(OUTPUT_DIR)/

# ---- Docker ----

.PHONY: docker
docker:
	@echo "▸ 构建 Docker 镜像 (linux/amd64)"
	docker build \
	  --platform linux/amd64 \
	  -t $(APP_NAME)-server:$(VERSION) \
	  -t $(APP_NAME)-server:latest \
	  -f deploy/docker/Dockerfile .
	@echo "✓ Docker 镜像构建完成"

.PHONY: docker-arm64
docker-arm64:
	@echo "▸ 构建 Docker 镜像 (linux/arm64)"
	docker build \
	  --platform linux/arm64 \
	  -t $(APP_NAME)-server:$(VERSION)-arm64 \
	  -t $(APP_NAME)-server:latest \
	  -f deploy/docker/Dockerfile .
	@echo "✓ Docker 镜像构建完成 (arm64)"

# ---- 代码质量 ----

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint 未安装，跳过"

.PHONY: test
test:
	go test -race -count=1 ./...

.PHONY: tidy
tidy:
	go mod tidy

# ---- 清理 ----

.PHONY: clean
clean:
	rm -rf $(OUTPUT_DIR)/
	@echo "✓ 清理完成"

# ---- 帮助 ----

.PHONY: help
help:
	@echo "fileupload 构建系统"
	@echo ""
	@echo "用法: make <target>"
	@echo ""
	@echo "编译:"
	@echo "  all                  编译前端 + server + cli"
	@echo "  web                  构建前端（npm install + build）"
	@echo "  web-dev              启动前端开发服务器"
	@echo "  server               编译当前平台 server"
	@echo "  cli                  编译当前平台 cli"
	@echo "  release              编译全部平台二进制"
	@echo "  server-linux-amd64   编译 linux/amd64 server"
	@echo "  server-linux-arm64   编译 linux/arm64 server"
	@echo "  cli-linux-amd64      编译 linux/amd64 cli"
	@echo "  cli-linux-arm64      编译 linux/arm64 cli"
	@echo ""
	@echo "Docker:"
	@echo "  docker               构建 Docker 镜像 (linux/amd64)"
	@echo "  docker-arm64         构建 Docker 镜像 (linux/arm64)"
	@echo ""
	@echo "代码质量:"
	@echo "  vet                  go vet"
	@echo "  lint                 golangci-lint"
	@echo "  test                 运行测试 (-race)"
	@echo "  tidy                 go mod tidy"
	@echo ""
	@echo "其他:"
	@echo "  clean                清理构建产物"
	@echo "  help                 显示此帮助"
