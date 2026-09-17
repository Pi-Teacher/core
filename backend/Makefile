# Pi Teacher 服务端开发任务.
#
# 项目要求 Go 1.26.7; GOTOOLCHAIN 让旧版本本地工具链自动拉取指定版本,
# 避免每个开发者手工管理多套 Go 安装.

BINARY      := pi-teacher-server
CMD         := ./cmd/pi-teacher-server
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X github.com/Pi-Teacher/server/internal/platform/version.Version=$(VERSION)

DB_DRIVER ?= sqlite
DB_DSN    ?= ./data/pi-teacher.db

.PHONY: help build test test-race lint fmt vet staticcheck migrate run clean tidy

help: ## 显示帮助.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## 构建服务端二进制到 ./bin.
	GOTOOLCHAIN=go1.26.7 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

test: ## 运行全部单元与集成测试 (SQLite).
	GOTOOLCHAIN=go1.26.7 go test ./...

test-race: ## 带竞态检测运行测试.
	GOTOOLCHAIN=go1.26.7 go test -race ./...

lint: fmt vet ## 格式化并静态检查.

fmt: ## 格式化全部 Go 源码.
	GOTOOLCHAIN=go1.26.7 gofmt -w .

vet: ## 运行 go vet.
	GOTOOLCHAIN=go1.26.7 go vet ./...

staticcheck: ## 运行 staticcheck (需以 Go 1.26 工具链构建).
	GOTOOLCHAIN=go1.26.7 go run honnef.co/go/tools/cmd/staticcheck@latest ./...

tidy: ## 整理 go.mod/go.sum.
	GOTOOLCHAIN=go1.26.7 go mod tidy

migrate: ## 按 DB_DRIVER/DB_DSN 执行迁移.
	GOTOOLCHAIN=go1.26.7 go run $(CMD) migrate --db-driver $(DB_DRIVER) --db-dsn "$(DB_DSN)"

run: ## 以本地默认配置启动服务.
	GOTOOLCHAIN=go1.26.7 go run $(CMD) serve --db-driver $(DB_DRIVER) --db-dsn "$(DB_DSN)" --listen :8080

clean: ## 清理构建产物.
	rm -rf $(BIN_DIR)
