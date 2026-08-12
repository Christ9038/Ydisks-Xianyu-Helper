# Ydisks-Xianyu-Helper 常用命令入口。详见各 target 注释。

GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: build build-int build-browser-install build-tray test test-int vet lint cover tidy frontend fmt check

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

## test-int: 带 integration tag 跑测试（含 browser，需 Chromium）
test-int:
	$(GO) test -tags integration ./...

## vet: go vet
vet:
	$(GO) vet ./...

## lint: golangci-lint（需先安装：brew install golangci-lint 或见 README）
lint:
	$(GOLANGCI_LINT) run ./...

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

## check: 本地提交前全套检查（fmt + vet + lint + test）
check: fmt vet lint test
