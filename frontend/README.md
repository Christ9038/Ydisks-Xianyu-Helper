# Ydisks闲鱼助手前端

React 19、Vite 6 与 TypeScript 单页应用，是 Go 后端提供的管理界面。

## 目录结构

```text
frontend/
  index.html                 Vite 入口 HTML
  index.tsx                  React 挂载入口
  App.tsx                    根组件，仅装配 Provider、路由与错误边界
  app/
    providers/               会话状态与初始化流程
    router/                  浏览器历史路由与访问控制
    shell/                   已登录应用壳与导航
    features/                按业务领域组织的页面、状态、Hook 和 API 适配器
  shared/
    api-contract/            版本化 HTTP DTO 定义
    http/                    统一 HTTP 客户端、错误解析与契约校验
    async/                   取消和最新请求代次工具
    browser/                 浏览器侧轻量持久化工具
    ui/                      跨领域复用的展示组件
  vite.config.ts             `base=/static/` 与 `/api`、`/health` 开发代理
```

生产组件、Hook 和状态只能通过所属 feature 的 API adapter 使用 transport DTO；feature 之间不得导入彼此的内部文件。所有生产业务请求均使用 `/api/v1/...`，并由 `frontend/featureArchitecture.test.ts` 的架构测试约束。

## 开发

```bash
cd /Users/christ/Workspace/git/xianyu/Ydisks-Xianyu-Helper/frontend
npm ci
npm run dev
```

Vite 开发服务器运行在 `http://localhost:3000`，将 `/api` 和 `/health` 代理到 `http://localhost:59188`。先启动后端，例如：

```bash
go run ./cmd/server -addr :59188
```

`-addr :59188` 会监听全部网络接口；本机开发以外应显式使用回环地址或配置网络访问控制。桌面安装包固定绑定 `127.0.0.1:59188`。

## 构建产物

```bash
npm run build
```

产物写入 `../internal/webui/static/`，随后在构建 Go 服务时由 `//go:embed` 嵌入并服务于 `/static/*`。生产部署无需单独分发前端。若服务二进制已运行，必须重新构建并重启服务，浏览器刷新不会替换已嵌入的资源。

侧边栏的运行版本和短提交号来自后端 `/health`。源码运行通常显示 `dev`/`unknown`；CI 构建的安装包和 Docker 镜像会注入版本信息。

## 路由

路由使用 `window.history.pushState`，包括 `/app/dashboard`、`/app/accounts`、`/app/chat`、`/app/cards`、`/app/items`、`/app/orders`、`/app/rules`、`/app/notifications` 与管理员可见的 `/app/settings`。未登录时显示会话表单；当 `/api/v1/session` 表示系统未初始化时，显示首次初始化表单。后端对非 API 的 GET 请求回退到 `index.html`，支持深链刷新。

## 测试

```bash
npm test -- --run
npm run typecheck
npm run comments:check
npm run build
```
