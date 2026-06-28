# 闲鱼管家

这是当前活跃版本：Go 后端 + React 前端。

旧目录 `../xianyu-super-butler/` 是历史 Python/FastAPI 版本。当前 `xianyu-go` 仓库已经包含自己的前端源码，后续功能迭代应改这里：

```text
xianyu-go/
  cmd/server/              Go 服务入口
  internal/                Go 后端代码
  frontend/                当前活跃 React 前端源码
  internal/webui/static/   前端构建产物，Go 会内置并提供给浏览器
```

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
go run ./cmd/server -init-admin -db data/xianyu_data.db
```

也可以非交互传密码：

```bash
go run ./cmd/server \
  -init-admin \
  -db data/xianyu_data.db \
  -admin-password '你的密码'
```

### 3. 启动服务

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
go run ./cmd/server -db data/xianyu_data.db -addr :8080
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

## 常用参数

| 参数 | 说明 |
| --- | --- |
| `-db` | SQLite 数据库路径，默认 `data/xianyu_data.db` |
| `-addr` | HTTP 监听地址，默认 `:8080` |
| `-web` | 外部前端静态资源目录；不传则用内置前端 |
| `-init-admin` | 初始化或重置管理员后退出 |
| `-admin-email` | 初始化管理员邮箱 |
| `-admin-password` | 初始化管理员密码 |
| `-no-browser` | 禁用内置浏览器自动化 |
| `-secure` | HTTPS 模式，Cookie 加 Secure |
| `-v` | 调试日志 |

## 验证

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
go test ./...

cd /Users/christ/Workspace/git/xianyu/xianyu-go/frontend
npm run build
```

