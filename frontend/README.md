# Ydisks闲鱼助手前端

React + Vite + TypeScript 单页应用，作为 Ydisks闲鱼助手 Go 后端的管理面板。

## 目录结构

```text
frontend/
  index.html           入口 HTML
  index.tsx            应用入口（挂载 React、登录态判断、tab 路由）
  App.tsx              主壳：侧边栏 + 内容区 + 登录/首次初始化
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
生产部署无需单独分发前端。若 Go 服务已经运行，构建完成后必须重启 Go 服务，
因为嵌入资源在 Go 编译时写入服务二进制；仅刷新浏览器不会更新已运行进程中的前端版本。

## Dashboard 图表约定

Dashboard 的营收趋势柱状图使用统一的品牌蓝 `#0094f7` 表示同一指标序列，日期之间
只通过柱高区分营收，不使用不同颜色表达不同类别。饼图使用中心汇总值、图例和悬浮提示
展示明细；悬停扇区会轻微放大。图表是展示型组件，点击或键盘焦点不会显示浏览器默认
的粗蓝色 SVG 焦点框，避免被误解为业务选中状态。

## 路由

应用使用 `window.history.pushState` 做 tab 导航，路径包括 `/app/dashboard`、`/app/accounts`、
`/app/chat`、`/app/cards`、`/app/items`、`/app/orders`、`/app/rules`、
`/app/notifications` 和管理员可见的 `/app/settings`。
未登录时显示登录表单（客户端状态，非独立路由）。当后端 `/verify` 返回
`initialized: false` 时，显示首次初始化表单；用户确认不少于 8 个字符的管理员密码后，
前端调用 `/initialize` 创建 `admin` 并使用返回的会话自动进入系统。后端 SPA catch-all
对非 API 的 GET 请求返回 `index.html`，支持深链刷新。

## 测试

```bash
npm test             # 前端单元测试
npm run typecheck    # TypeScript 类型检查
npm run build        # 构建并更新 Go 嵌入资源
```
