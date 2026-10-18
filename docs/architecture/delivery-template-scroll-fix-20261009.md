# 发货模板编辑器滚动修复（2026-10-09）

## 范围与原因

- `fieldset` 在当前 flex + max-height 布局中没有形成可用内容滚动区；外框虽收缩，消息末尾及添加按钮被外层 `overflow:hidden` 裁剪。
- 普通 div 独占滚动职责，内层 fieldset 保留保存时整组禁用，不改请求、草稿保存、API 或发货流程。
- 添加消息移入固定页脚并显式跟随 saving 禁用；窄屏独占一行，取消和保存保持可达。
- 新增时记录一次性草稿身份，节点提交后聚焦并调用 `scrollIntoView({ block: 'nearest' })`。取消清除待聚焦身份；排序和普通编辑不抢焦点。
- 没有新增或扩大注释基线，中文注释门禁覆盖本次全部新增声明；冻结 CAPTCHA 文件及行为未改。

## 回归范围

- React/jsdom：原有8项回归保留；新增连续添加到11条、滚动调用、焦点、取消重开、保存中禁用、失败解锁；迟到请求不能解锁新的添加入口。
- 真实 Chromium：直接装载 `internal/webui/static` 的生产组件与 CSS，只替换本地模板数据，不注入修复样式、不复制生产组件、不调用真实平台。
- 24种组合：1366×768、1920×1080、390×844、320×568 × 3/5/10条 × 文本/交替图文；验证真实滚轮移动、固定按钮命中、连续新增两条并完整展示输入框、保存与取消。
- 浏览器回归先在旧构建上失败，再在修复构建上通过。第一轮修复还暴露390×844仅原生focus时输入框部分遮挡，补显式滚动后通过，未放宽断言。
- `make cover-browser` 增加 `internal/webui`，并以 `-p 1` 串行执行两个使用 Chromium 的包，避免墙钟测试争用宿主CPU。

## 验证结果

以下是在 Windows PowerShell 中执行的等价命令；覆盖率文件均为生成物，不提交。

| 命令 | 结果 |
| --- | --- |
| `npm --prefix frontend test -- app/features/delivery-templates/pages/DeliveryTemplates.test.tsx` | 10/10通过 |
| `$env:RUN_BROWSER_INTEGRATION='1'; go test ./internal/webui -run TestDeliveryTemplateEditorBrowserIntegration -count=1 -timeout=180s` | 24/24场景通过；真实本地Chromium 149.0.7827.55 |
| `npm --prefix frontend run test:coverage`（等价`make cover-frontend`） | 99文件、661测试通过；V8 statement **80.89%** |
| `go test -count=1 -timeout=600s -coverprofile cover.out ./...`（等价`make cover`） | 通过；未设置`RUN_BROWSER_INTEGRATION=1`；Go statement **81.5%** |
| `$env:RUN_BROWSER_INTEGRATION='1'; go test -p 1 -count=1 -timeout=600s -coverprofile cover-browser.out ./internal/browser ./internal/webui` | **整体未通过**；webui通过，既有冻结滑块测试耗时断言失败；该失败运行的合并statement为64.1%，不能视作全套通过证据 |
| `go run ./tools/commentlint -mode check -root .`、`npm --prefix frontend run comments:check` | 通过，未修改任何基线 |
| `npm --prefix frontend run typecheck`、`go run ./tools/architecturecheck` | 通过 |
| `go run ./tools/apicheck`、`npm --prefix frontend run api:check` | 通过，生成契约无漂移 |
| `go vet ./internal/webui`、`git diff --check` | 通过 |
| `npm --prefix frontend run build`、`go build -o .planning/2026-10-09-template-scroll-analysis/xianyu-server.exe ./cmd/server` | 通过，前端嵌入资源已同步 |

### 未通过项目与排除范围

- 全套浏览器中 `TestReferencePlaywrightSliderBrowserIntegration` 在当前机器仍失败：最终串行运行 `moves=88, duration_ms=1586, vertical_span=22, overshoot=5`，失败位置为冻结测试 `token_captcha_fallback_integration_test.go:125`。并行时曾为1376ms，首次串行为1651ms；串行不能消除此问题，不能将原因断言为CPU竞争。没有修改冻结实现、超时、阈值或断言，也没有跳过该用例。
- 本次变更的新增/禁用/失败恢复/取消/焦点行为为确定性工作，已由页面测试和真实浏览器覆盖；原有页面删除、部分校验与键盘分支的剩余覆盖率属于既有确定性测试补齐工作，不新增排除或降低阈值。V8中的DOM几何不以jsdom模拟通过，实际交互由Chromium回归验证。
- 环境相关项：冻结滑块墙钟断言如上；未运行MySQL/PostgreSQL外部服务回归、race或golangci-lint，本次未改数据库或Go业务。
- 真实账号/外部平台项：未验证真实闲鱼登录、风控、发货投递、图片上传和外部通知渠道；本次浏览器夹具不访问这些服务。没有把本地布局测试列为真实账号例外。
- 初次覆盖率命令的未引用`-coverprofile=*.out`被PowerShell拆出`.out`伪包，已改用独立参数并重跑；保留失败日志，删除误生成的无扩展名覆盖率文件。不把旧报告或失败命令当作成功结果。

本文记录提交前的本地验证基线。2026-10-10 用户已授权与商品默认回复单会话选项一并提交、推送并构建镜像；发布结论以仓库 Actions 为准。
