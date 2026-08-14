# Ydisks-Xianyu-Helper 常用命令入口。详见各 target 注释。

GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: build build-int build-browser-install build-tray test test-server test-server-race test-int vet lint architecture cover tidy frontend fmt comments comments-baseline check

## build: 编译 server（默认，跳过 integration build tag）
build:
	$(GO) build ./cmd/server

## build-browser-install: 编译独立的 Chromium 安装辅助程序
build-browser-install:
	$(GO) build ./cmd/browser-install

## build-tray: 编译 Windows/macOS 菜单栏控制器（需在目标桌面系统上编译）
build-tray:
	$(GO) build ./cmd/tray

## build-int: 带 integration tag 编译（含 browser 包，需要 Chromium 环境）
build-int:
	$(GO) build -tags integration ./...

## test: 跑全部单元测试（默认跳过 browser 集成包）
test:
	$(GO) test ./...

## test-server: 跑 HTTP server 全量单元测试
test-server:
	$(GO) test ./internal/server

## test-server-race: 跑已验证的 server 生命周期与凭证并发 race 子集
test-server-race:
	$(GO) test -race ./internal/server -run 'TestRun_|TestPublishWorkerTrackingWaitsForCompletion|TestPublishRecoveryLifecycleStopsBeforeWorkerWait|TestUpdateRunningCookieWakesCredentialBlockedAutomationWithoutManager|TestSetCookieStatusWaitsForCredentialTransition|TestDeleteCookieRechecksOwnershipInsideCredentialLock'

## test-int: 带 integration tag 跑测试（含 browser，需 Chromium）
test-int:
	$(GO) test -tags integration ./...

## vet: go vet
vet:
	$(GO) vet ./...

## lint: golangci-lint（需先安装：brew install golangci-lint 或见 README）
lint:
	$(GOLANGCI_LINT) run ./...

## architecture: 检查 Go 低层依赖方向和 Server 事务边界
architecture:
	$(GO) run ./tools/architecturecheck

## cover: 生成覆盖率报告
cover:
	$(GO) test -coverprofile=cover.out ./... && $(GO) tool cover -func=cover.out | tail -1

## fmt: 格式化所有 Go 源码
fmt:
	$(GO) fmt ./...

## tidy: 整理 go.mod
tidy:
	$(GO) mod tidy

## frontend: 安装依赖并构建前端到 internal/webui/static/
frontend:
	cd frontend && npm ci && npm run build

## comments: 检查 Go 与 TypeScript/TSX 新增声明是否有中文注释
comments:
	$(GO) run ./tools/commentlint -mode check -root . -baseline .commentlint/go-baseline.json
	node frontend/scripts/check-comments.mjs --mode check --root frontend --baseline .commentlint/frontend-baseline.json

## comments-baseline: 根据当前代码生成一次性历史问题基线（仅在审查后使用）
comments-baseline:
	mkdir -p .commentlint
	$(GO) run ./tools/commentlint -mode baseline -root . -baseline .commentlint/go-baseline.json
	node frontend/scripts/check-comments.mjs --mode baseline --root frontend --baseline .commentlint/frontend-baseline.json

## check: 本地提交前全套检查（fmt + vet + lint + test）
check: fmt architecture vet lint test comments
