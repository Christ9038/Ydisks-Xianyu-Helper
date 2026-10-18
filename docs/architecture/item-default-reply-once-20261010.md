# 商品默认回复：单个会话只回复一次（2026-10-10）

## 使用与兼容语义

- 自动化规则 → 商品默认回复 → 新增/编辑，勾选“单个会话只回复一次”。默认不勾选，历史配置继续每次回复。
- 仅在消息进入默认回复阶段时应用此选项，关键词/API/AI回复优先级不变。勾选后按**账号＋商品＋会话**持久化去重；另一商品、另一会话、另一账号仍可独立回复。账号兜底的启用和 once 开关不影响商品配置，商品记录也不消耗账号兜底记录。
- 纯文字、URL图片、本地图片、图文都遵守相同规则。图文部分成功后仅补未确认的分段；结果不确定时隔离，不自动重放。
- 修改内容、重启进程或关闭再开启选项不清空既有发送记录。关闭时恢复每次回复；清空账号兜底记录只处理账号作用域，不清空商品记录。
- 商品图文全部为空时仍继承账号兜底，不因为商品选项而抑制账号回复；缺少会话身份的商品单次回复停止投递，不能绕过去重。

## 实现与迁移

- `/api/v1/reply-rules/items/{cookie_id}/{item_id}` 写入新增可选布尔字段 `reply_once`；旧兼容路由共用应用逻辑。省略字段在账号事务内保留已有值，新建默认 false；显式 false 可关闭。单条和列表响应带该字段，旧响应在前端归一为 false。
- SQLite/MySQL/PostgreSQL 对齐迁移 `00057_item_reply_once.sql`（在 v1.0.14 基线上重编号：上游已占用至 `00054_keyword_expressions`，本功能的图片迁移占用 `00056_reply_images`）：`item_replay.reply_once` 默认0，投递记录增加空串默认 `item_id`，唯一性改为 `(cookie_id,chat_id,item_id)`。
- SQLite逐列复制原记录的id、时间、状态、确认分段、错误和租约。空item_id表示原账号兜底；降级只保留旧版本能表达的账号记录，不把商品记录混入旧账号去重。
- `RecordsForItem` 返回无共享可变状态的范围仓储；现有账号调用方法保留兼容包装器，委托空范围。原原子领取、确定失败与未知结果处理共用一份实现，不复制独立状态机。
- 不增加原始数据库访问、业务HTTP旁路、凭据读取、后台协程或运行时依赖setter。未改冻结滑块、自动发货顺序、上轮模板滚动修复或注释基线。

## 验证

Windows PowerShell使用以下等价命令；`cover*.out`和`frontend/coverage/`均为生成物，不提交。

| 命令 | 结果 |
| --- | --- |
| `npm --prefix frontend run test:coverage`（`make cover-frontend`） | 99文件、663测试通过；V8 statement **80.89%** |
| `go test -count=1 -timeout=600s -coverprofile cover.out ./...`（`make cover`） | 普通Go全库通过；未设置`RUN_BROWSER_INTEGRATION=1`；Go statement **81.5%**；补充SQL故障回归后再次全量通过，共享投递记录仓储各函数statement均100% |
| `$env:RUN_BROWSER_INTEGRATION='1'; go test -p 1 -count=1 -timeout=600s -coverprofile cover-browser.out ./internal/browser ./internal/webui`（`make cover-browser`） | 本轮完整通过；Go statement **64.1%**；包括上轮模板滚动修复24种真实嵌入资源场景 |
| `go test ./internal/server -run 'TestItemReplyOnceContracts|TestReplyImageConfigurationContracts' -count=1` | 真实新旧handler和OpenAPI响应校验通过 |
| `go test ./internal/db ./internal/engine -run 'TestMultiDB_ItemReplyOnce|TestItemReplyOnce|TestItemDefaultReplyOnce' -count=1` | 配置兼容、并发领取、作用域隔离、迁移、重启去重、内容类型及失败保护通过 |
| `go run ./tools/commentlint -mode check -root .`、`npm --prefix frontend run comments:check` | 中文注释通过；未新增或扩大基线，清理本次记录状态及商品列表逻辑单元的历史占位注释 |
| `npm --prefix frontend run typecheck`、`go run ./tools/architecturecheck`、`go run ./tools/apicheck`、`npm --prefix frontend run api:check` | 全部通过，生成契约无漂移 |
| `go vet ./internal/db ./internal/application/keywords ./internal/adapter ./internal/engine ./internal/server`、`git diff --check` | 通过 |
| `npm --prefix frontend run build`、`go build -o .planning/2026-10-09-item-default-reply-once/xianyu-server.exe ./cmd/server` | 构建通过，包含两次修复的嵌入前端已同步 |

### 真实浏览器及错误路径

- 本地诊断脚本`.planning/2026-10-09-item-default-reply-once/browser-ui-probe.cjs`装载真实商品编辑器、Hook、API adapter源码及构建CSS，只替换本地无凭据HTTP数据。1366×768、390×844、320×568均完成开启→保存→重开回显→关闭→保存失败仍保留选择→重试保存→取消。该脚本是本地验证记录，永久行为回归保留在前端测试中。
- SQL故障测试通过已取消Context、忽略插入及拒绝更新的临时SQLite触发器，覆盖领取插入失败、快照缺失、更新失败和主/降级隔离同时失败；不能取得发送权时必须停发，保留sending状态不自动接管。
- 迁移测试验证旧账号的sent、sending、分段failed记录升级后不会被清空或串到商品范围，回退保留旧账号事实。旧关键词/图片迁移测试仍保留原精确断言，仅当前最新版本更新57；旧schema完成独立断言后再升级给当前仓储使用。
- 上轮浏览器套件曾有冻结滑块墙钟失败，本轮串行运行通过；没有修改冻结代码、超时或断言，也没有跳过该测试。

### 环境边界与剩余覆盖范围

- 新增配置、事务合并、去重和错误处理属于确定性工作，由SQLite、注入发送端口、真实handler及前端行为测试覆盖；未设置覆盖率忽略、降低阈值或扩大兼容名单。
- 三方言迁移同步提供；本机未配置`TEST_MYSQL_URL`、`TEST_POSTGRES_URL`，实际数据库回归仅SQLite，外部方言运行属于环境待验证项。未运行race和golangci-lint。
- 未调用真实闲鱼账号、实际买家会话、远端图片上传、登录/风控或外部通知服务；这些属于真实账号/外部平台验证项，而非本地UI和失败分支的豁免。
- 普通Go和V8覆盖率未覆盖的其他既有业务分支仍按总计划作为确定性测试补齐工作；纯UI的几何使用本地Chromium验证，不用jsdom的无布局结果冒充可见性证据。

本文记录提交前的本地验证基线。2026-10-10 用户已授权将本功能与上一轮模板滚动修复一并提交、推送并构建镜像；发布结论以仓库 Actions 为准。
