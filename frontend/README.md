# Ydisks闲鱼助手前端

React + Vite + TypeScript 单页应用，作为 Ydisks闲鱼助手 Go 后端的管理面板。

## 目录结构

```text
frontend/
  index.html           入口 HTML
  index.tsx            应用入口（挂载 React、登录态判断、tab 路由）
  App.tsx              主壳：侧边栏 + 内容区 + 登录/初始化
  components/           业务组件（Dashboard / OrderList / CardList / Rules / Settings ...）
  services/             后端 API 封装（fetch 调用）
  request.ts            统一请求工具（带 session cookie、错误处理）
  types.ts              共享类型定义
  vite.config.ts        Vite 配置（base=/static/，代理 /api 到 :8080）
```

## 开发

```bash
cd /Users/christ/Workspace/git/xianyu/Ydisks-Xianyu-Helper/frontend
npm install
npm run dev      # http://localhost:3000，API 代理到 localhost:8080
```

开发时先启动后端（`go run ./cmd/server -addr :8080`），再启动前端 dev server。

## 构建产物

```bash
npm run build
```

产物写入 `../internal/webui/static/`，由 Go 服务通过 `//go:embed` 内嵌并服务于 `/static/*`。
生产部署无需单独分发前端，构建一次即可。

## 路由

应用使用 `window.history.pushState` 做 tab 导航，路径形如 `/app/dashboard`、`/app/rules`、`/app/settings`。
未登录时显示登录表单（客户端状态，非独立路由）。后端 SPA catch-all 对非 API 的 GET 请求返回 `index.html`，支持深链刷新。

## 测试

```bash
npx vitest run     # 路由一致性、请求工具等单测
```
