# 闲鱼管家 Go 后端

这是闲鱼自动回复/自动发货系统的 Go 重写后端。

## 运行方式

### 1) 本地开发运行

先构建前端静态文件：

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-super-butler/frontend
npm install
npm run build
```

启动后端：

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
go run ./cmd/server \
  -db data/xianyu_data.db \
  -addr :8080 \
  -web /Users/christ/Workspace/git/xianyu/xianyu-super-butler/static
```

浏览器打开：

```text
http://localhost:8080
```

### 2) 首次初始化管理员

系统第一次启动前先初始化管理员：

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
go run ./cmd/server -init-admin -db data/xianyu_data.db
```

也可以指定密码：

```bash
go run ./cmd/server -init-admin -db data/xianyu_data.db -admin-password '你的密码'
```

### 3) 编译后运行

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-go
go build -o xianyu-server ./cmd/server
go build -o xianyu-init-admin ./cmd/init-admin
```

运行：

```bash
./xianyu-server -db data/xianyu_data.db -addr :8080 -web /Users/christ/Workspace/git/xianyu/xianyu-super-butler/static
```

### 4) Docker 部署

批量铺货会用到上传目录，建议挂载持久化数据：

```bash
docker run -v ./data:/app/data ...
```

并设置：

```bash
XIANYU_UPLOAD_DIR=/app/data/uploads
```

## 常用参数

| 参数 | 说明 |
| --- | --- |
| `-db` | SQLite 数据库路径，默认 `data/xianyu_data.db` |
| `-addr` | HTTP 监听地址，默认 `:8080` |
| `-web` | 前端静态资源目录（包含 `index.html`） |
| `-init-admin` | 初始化/重置管理员后退出 |
| `-admin-email` | 初始化管理员邮箱 |
| `-admin-password` | 初始化管理员密码 |
| `-no-browser` | 禁用内置浏览器自动化 |
| `-secure` | HTTPS 模式（Cookie 加 Secure） |
| `-v` | 输出调试日志 |

## 构建检查

```bash
go test ./...
```

前端：

```bash
cd /Users/christ/Workspace/git/xianyu/xianyu-super-butler/frontend
npm run build
```

## 目录说明

- `cmd/server`：主服务入口
- `cmd/init-admin`：管理员初始化 CLI
- `internal/db`：SQLite 和迁移
- `internal/server`：HTTP API
- `internal/engine`：账号运行时
- `internal/xianyu`：协议和 mtop 调用
- `internal/webui`：内置静态前端资源
