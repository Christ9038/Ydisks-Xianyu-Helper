# 闲鱼管家

这是当前活跃版本：Go 后端 + React 前端。后续功能迭代均在此目录进行：

```text
xianyu-go/
  cmd/server/              Go 服务入口（含 -init-admin 非交互初始化）
  cmd/init-admin/          交互式初始化管理员 CLI
  cmd/dbverify/            三库迁移 + 核心 CRUD 验证工具
  internal/                Go 后端代码
  frontend/                当前活跃 React 前端源码
  internal/webui/static/   前端构建产物，Go 会内置并提供给浏览器
```

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

批量铺货会用到上传目录。容器部署时建议挂载持久化数据：

```bash
-v ./data:/app/data
```

并设置：

```bash
XIANYU_UPLOAD_DIR=/app/data/uploads
```

数据库建议放在：

```text
/app/data/xianyu_data.db
```

外置 MySQL/Postgres 时改用 `DATABASE_URL` 环境变量。

## 常用参数

| 参数 | 说明 |
| --- | --- |
| `-db` | SQLite 数据库路径，默认 `data/xianyu_data.db`（向后兼容） |
| `-db-url` | 数据库连接 URL（`sqlite://` / `mysql://` / `postgres://`），优先级高于 `-db` |
| `DATABASE_URL` | 环境变量，优先级最高 |
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
Cookie、买家 ID 和卡密内容。测试应用分别监听 `18081`（MySQL）与 `18082`
（PostgreSQL），测试账号为 `docker_fixture / docker_fixture_password`。

保留数据库并停止容器：

```bash
docker compose -f docker-compose.functional.yml down
```

只有明确需要清空测试数据时才追加 `-v` 删除命名卷。
