# 闲鱼管家

这是当前活跃版本：Go 后端 + React 前端。后续功能迭代均在此目录进行：

```text
xianyu-go/
  cmd/
    server/                服务入口、依赖装配、管理员非交互初始化
    init-admin/            交互式管理员初始化 CLI
    dbverify/              SQLite/MySQL/Postgres 迁移与 CRUD 验证
    dbseed/                Docker 功能测试所需的脱敏数据种子工具
    spike/                 debug/tools 构建标签下的协议链路探针
  internal/
    account/               已启用账号监督与生命周期管理
    adapter/               engine/automation 与浏览器、通知器的接线层
    auth/                  管理端会话认证与安全 Cookie
    automation/            发货、评价赠送、邀评自动化与调度器
    browser/               Playwright/Chromium 登录、风控验证、订单抓取
    db/                    三种数据库方言、模型、存储与嵌入式迁移
    engine/                单账号运行时、消息处理、回复和发货行为
    logging/               进程级结构化日志配置
    logsafe/               敏感 ID 与 URL 的日志脱敏
    netguard/              出站网络目标校验与 DNS rebinding 防护
    notify/                账号告警和通知渠道
    renewal/               登录态续期调度与冷却控制
    server/                chi HTTP API、鉴权和 SPA 服务
    webui/static/          前端构建产物，由 Go 二进制嵌入
    xianyu/                MTOP、WebSocket、扫码登录和协议实现
  frontend/                当前活跃 React/Vite 前端源码
  docs/                    运行、功能与不可变行为规范
  scripts/                 Docker、持久化和功能回归脚本
  data/                    本地运行数据（不作为源码维护）
```

目录职责以根目录 `AGENTS.md` 为准。滑块认证属于冻结逻辑，完整契约见
[`docs/slider-captcha-frozen-spec.md`](docs/slider-captcha-frozen-spec.md)；没有用户在当前任务中的明确授权，不得修改其实现、测试或任何会间接改变其行为的调用链。

## 数据库支持

支持三种数据库，由连接 URL 的 scheme 决定：

| scheme | 说明 |
| --- | --- |
| `sqlite://<path>` 或纯路径 | 纯 Go modernc.org/sqlite，WAL + foreign_keys（默认，本地开发） |
| `mysql://<dsn>` | go-sql-driver/mysql（生产 / Docker 外置库） |
| `postgres://<dsn>` | jackc/pgx（生产 / Docker 外置库） |

迁移用 goose 嵌入式执行，按方言分目录 `internal/db/migrations/{sqlite,mysql,postgres}`。

## 首次运行

### 1. 安装前端依赖并构建

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go/frontend
npm install
npm run build
```

构建产物会写入：

```text
xianyu-go/internal/webui/static/
```

### 2. 初始化管理员

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
go run ./cmd/init-admin -db data/xianyu_data.db
```

也可以通过服务入口非交互初始化（适合脚本/Docker）：

```bash
go run ./cmd/server \
  -init-admin \
  -db data/xianyu_data.db \
  -admin-password '你的密码'
```

`-admin-password` 也可用环境变量 `XIANYU_ADMIN_PASSWORD` 传入。

### 3. 启动服务

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
go run ./cmd/server -db data/xianyu_data.db -addr :8080
```

使用 MySQL / Postgres 时改用 `-db-url` 或 `DATABASE_URL`：

```bash
# MySQL（DSN 需含 multiStatements=true，工具会自动补齐）
go run ./cmd/server -db-url "mysql://user:pass@tcp(host:3306)/dbname" -addr :8080

# Postgres
DATABASE_URL="postgres://user:pass@host:5432/dbname?sslmode=disable" go run ./cmd/server -addr :8080
```

浏览器访问：

```text
http://localhost:8080
```

不传 `-web` 时，Go 会使用 `internal/webui/static` 中的内置前端资源。

## 开发模式

后端：

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
go run ./cmd/server -db data/xianyu_data.db -addr :8080
```

前端开发服务器：

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go/frontend
npm run dev
```

访问：

```text
http://localhost:3000
```

Vite 会把 API 请求代理到 `http://localhost:8080`。

## 编译运行

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
go build -o xianyu-server ./cmd/server
./xianyu-server -db data/xianyu_data.db -addr :8080
```

如果要使用外部前端目录，也可以传：

```bash
./xianyu-server -db data/xianyu_data.db -addr :8080 -web /path/to/static
```

## 验证数据库（三库冒烟）

`cmd/dbverify` 在目标库上跑迁移 + 核心 CRUD，确认方言适配正常：

```bash
go run ./cmd/dbverify "sqlite:///tmp/verify.db"
go run ./cmd/dbverify "mysql://root:pass@tcp(host:3306)/db?multiStatements=true"
go run ./cmd/dbverify "postgres://user:pass@host:5432/db"
```

全部 9 步通过即说明 upsert / 布尔读写 / 自增主键 / NULL 扫描跨三库一致。

## Docker 部署注意

仓库提供 `docker-compose.debian13-postgres17.yml`，用于运行 Debian 13 应用镜像和
PostgreSQL 17。GHCR 中的同一镜像标签同时包含 `linux/amd64` 和 `linux/arm64`，
Docker 会根据宿主机 CPU 自动选择：x86_64 Linux 拉取 amd64，Apple Silicon 拉取
arm64，无需手工设置 `platform`。

```bash
cp .env.example .env
# 编辑 .env，至少替换 PostgreSQL 密码、DATABASE_URL 和 XIANYU_DATA_KEY
docker login ghcr.io -u Christ9038
docker compose -f docker-compose.debian13-postgres17.yml pull app postgres
docker compose -f docker-compose.debian13-postgres17.yml up -d --no-build postgres app
```

`dev` 分支会由 GitHub Actions 自动发布多架构的
`ghcr.io/christ9038/xinayu-go:dev`；`main` 分支同时发布 `main` 和 `latest` 标签，
版本标签（例如 `v1.2.3`）会发布对应语义化版本标签。仓库和镜像为私有时，服务器
登录 GHCR 所用的 Personal Access Token 需要 `read:packages` 权限。本地修改镜像后
也可以运行 `docker compose -f docker-compose.debian13-postgres17.yml build app` 自行构建。

数据库只在 Compose 内部网络开放，不映射宿主机端口；应用端口由
`XIANYU_BIND_ADDRESS` 和 `XIANYU_HTTP_PORT` 控制。PostgreSQL 数据、应用数据和
Chromium 持久化配置分别保存在命名卷 `postgres_data`、`app_data` 和
`browser_data` 中。

首次部署后初始化管理员：

```bash
docker compose -f docker-compose.debian13-postgres17.yml --profile init run --rm init-admin
```

检查服务和日志：

```bash
docker compose -f docker-compose.debian13-postgres17.yml ps
curl -fsS http://127.0.0.1:8080/health
docker compose -f docker-compose.debian13-postgres17.yml logs -f app
```

`.env` 已被 Git 和 Docker 构建上下文忽略。`DATABASE_URL` 必须使用 Compose 服务名
`postgres` 作为主机；如果密码包含 `@`、`:`、`/` 等字符，需要在连接串中进行 URL
编码。部署到服务器前可用 `openssl rand -base64 48` 生成稳定的
`XIANYU_DATA_KEY`，并与数据库卷一同安全备份。

生产环境建议同时设置稳定且仅由服务进程可读的 `XIANYU_DATA_KEY`。启用后，Cookie、
账号登录密码、设备/访问令牌、AI/SMTP 密钥和通知渠道配置会使用 AES-256-GCM 加密
后落库；首次启动会自动升级历史明文。该密钥必须随数据卷一同备份，丢失或更换后
已有凭证无法解密。

管理员配置的 OpenAI 兼容 AI Base URL 不限制目标网络，可使用 `0.0.0.0`、loopback、
Docker 服务名、RFC1918 私网、Tailscale/CGNAT、IPv6 ULA 或公网地址，便于连接任意部署
位置的 Ollama、vLLM、LocalAI 或兼容网关。通知 Webhook、远程图片等非管理员可信目标
继续只允许公网地址。

## 常用参数

| 参数 | 说明 |
| --- | --- |
| `-db` | SQLite 数据库路径，默认 `data/xianyu_data.db`（向后兼容） |
| `-db-url` | 数据库连接 URL（`sqlite://` / `mysql://` / `postgres://`），优先级高于 `-db` |
| `DATABASE_URL` | 环境变量，优先级最高 |
| `XIANYU_DATA_KEY` | 敏感字段加密主密钥；生产环境应固定配置并安全备份 |
| `-addr` | HTTP 监听地址，默认 `:8080` |
| `-web` | 外部前端静态资源目录；不传则用内置前端 |
| `-init-admin` | 初始化或重置管理员后退出 |
| `-admin-email` | 初始化管理员邮箱 |
| `-admin-password` | 初始化管理员密码（也可用 `XIANYU_ADMIN_PASSWORD` 环境变量） |
| `-no-browser` | 禁用内置浏览器自动化（扫码风控/密码登录/订单抓取将不可用） |
| `-secure` | HTTPS 模式，Cookie 加 Secure |
| `-v` | 调试日志 |

## 质量门禁与测试

项目内置 Makefile 与 golangci-lint 配置：

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
make vet        # go vet
make test       # 全部单元测试
make lint       # golangci-lint（0 issues 基线）
make cover      # 覆盖率报告
make check      # vet + lint + test 一键全检
make frontend   # 构建前端
```

跨三库回归（需 Docker 容器或外置库）：

```bash
TEST_MYSQL_URL="mysql://root:pass@tcp(host:3306)/db" \
TEST_POSTGRES_URL="postgres://user:pass@host:5432/db" \
go test ./internal/db -run TestMultiDB -v
```

完整 Docker 功能回归（前端测试/构建、vet、lint、Go `-race`、三库迁移、
MySQL 8.4 与 PostgreSQL 17 API 功能及重启持久化）：

```bash
./scripts/docker-full-test.sh
```

测试编排使用命名卷 `mysql8_data`、`postgres17_data` 和 `sqlite_seed`。
本地 `data/xianyu_data.db` 只会抽取少量商品、订单和卡密元数据，写入外置库前会替换
Cookie、买家 ID 和卡密内容。测试应用分别监听 `28081`（MySQL）与 `28082`
（PostgreSQL），测试账号为 `docker_fixture / docker_fixture_password`。
测试管理员为 `docker_admin / docker_admin_password`。

保留数据库并停止容器：

```bash
docker compose -f docker-compose.functional.yml down
```

只有明确需要清空测试数据时才追加 `-v` 删除命名卷。
