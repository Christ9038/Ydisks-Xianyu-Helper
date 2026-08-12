# Ydisks闲鱼助手

![Ydisks闲鱼助手Slogan](https://raw.githubusercontent.com/Christ9038/Ydisks-Xianyu-Helper/main/docs/img/slogan.png)

基于 Go 与 React 构建的闲鱼多账号管理、消息回复与自动发货系统

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](frontend/package.json)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white)](compose.yml)
[![Docker Image](https://github.com/Christ9038/Ydisks-Xianyu-Helper/actions/workflows/docker-publish.yml/badge.svg?branch=main)](https://github.com/Christ9038/Ydisks-Xianyu-Helper/actions/workflows/docker-publish.yml)

[功能特性](#功能特性) · [快速开始](#快速开始) · [配置说明](#配置说明) ·
[Docker 部署](#docker-部署) · [开发指南](#开发指南) · [开源协议](#开源协议)

操作手册见仓库内的 [GitHub Wiki 文档](docs/wiki/Home.md)：包含数据库选择、账号接入、
在线聊天、账号自动任务、批量铺货、自动化发货、AI、通知和运维流程。

> [!IMPORTANT]
> 本项目是社区维护的非官方工具，与闲鱼、阿里巴巴集团及其关联公司无隶属、合作或
> 授权关系。请仅在遵守当地法律法规、闲鱼平台规则及账号授权范围的前提下使用。
> 使用自动化能力可能触发平台风控，请自行评估风险并妥善保护账号凭证。

## 项目简介

Ydisks闲鱼助手是一个面向闲鱼卖家的自托管管理系统。它将账号运行、即时消息、订单、
商品、卡密库存、自动化规则、AI 回复和异常通知整合到同一个 Web 管理后台，适合需要
同时维护多个闲鱼账号或交付虚拟商品的个人与小型团队。

项目采用 Go 语言实现闲鱼登录、Cookie 续期、MTOP 请求和 WebSocket 消息链路。
扫码登录、人脸验证流程、消息连接、凭证更新和绝大部分业务逻辑均由 Go 客户端完成。
使用 Chromium 处理必须依赖浏览器环境的滑块风控。

### 与 Ydisks 网盘拉新助手协同使用

如果你通过网盘资料、教程包或数字资源做闲鱼推广，推荐搭配
[Ydisks 网盘拉新助手](https://www.ydisks.com) 使用：先为不同商品、渠道或推广账号
创建独立短链，再在 Ydisks 控制台查看短链与目标页面的 PV、UV、趋势、排行及渠道/账号报表。
这样，Ydisks 负责识别“链接从哪里带来访问与转化线索”，Ydisks闲鱼助手负责承接账号消息、
订单和自动发货，帮助你把投放复盘与日常交付放进一套更清晰的运营流程。

Ydisks 支持管理渠道、推广账号、原始链接、推广短链与域名，适用于网盘拉新和推广投放场景；
可从 [Ydisks 网盘拉新助手使用文档](https://docs.ydisks.com/guide/quick-start) 开始创建推广链接并查看数据。


### 适用场景

- 多个闲鱼账号的统一在线状态与消息管理
- 数字商品、兑换码、链接或图片内容的自动交付
- 付款后发货、评价赠品、超时求评价等自动化流程
- 商品与订单同步、单商品发布及支持逐行/默认类目、自动识别和最终兜底的表格批量铺货
- 接入 OpenAI 兼容接口实现可控的 AI 客服回复
- 账号掉线、风控、续期和发货结果的多渠道通知

## 功能特性

| 模块 | 能力 |
| --- | --- |
| 账号管理 | 多账号启停、扫码登录、密码登录、资料刷新、在线状态、登录审计与备注管理 |
| 凭证续期 | Cookie/token 定时续期、WebSocket 凭证更新、失效恢复、冷却与失败停用 |
| 即时消息 | 按账号隔离的在线聊天、会话列表与历史消息、闲鱼表情、文本/图片收发、官方系统消息识别、关键词回复、默认回复及仅回复一次 |
| 自动化中心 | 付款后自动发货、评价后赠品、超时未评价提醒、失败任务恢复与幂等检查点 |
| 账号自动任务 | 账号级持续扫描待评价订单并统一好评、按北京时间每日擦亮、手动立即执行、执行记录与幂等保护 |
| 卡密库存 | 文本、批量卡密和图片三种交付类型，库存追加、批量导入、规格与延迟发送 |
| 商品管理 | 商品同步、手工关联、单商品发布、CSV + ZIP 批量铺货、关键词获取默认类目、逐行类目优先、自动识别与“电子资料”最终兜底、任务恢复与结果导出 |
| 订单管理 | 订单同步、插入、编辑、平台发货、补发卡密、仅确认发货及异常状态处理 |
| AI 回复 | OpenAI 兼容 API、模型发现、自定义提示词、议价轮次和让价范围控制 |
| 数据看板 | 活跃账号、订单、营收、库存、商品销量与金额统计 |
| 通知告警 | Bark、钉钉、飞书、企业微信、Telegram、邮件和自定义 Webhook |
| 数据存储 | SQLite、MySQL、PostgreSQL，内置按方言执行的 Goose 数据库迁移 |
| 安全能力 | 管理端会话、敏感字段 AES-256-GCM 加密、日志脱敏和出站地址校验 |
| 容器部署 | PostgreSQL 17、健康检查、持久化卷、GHCR amd64/arm64 多架构镜像 |

批量铺货的类目优先级为：表格当前行指定类目 > 上传页通过关键词获取的默认类目 > 闲鱼按标题、描述和图片自动识别 > “电子资料”最终兜底。模板类目字段可全部留空；如填写，类目 ID、名称和频道类目 ID 必须同时填写，淘宝类目 ID 按闲鱼响应提供的值填写。

## 系统架构

```mermaid
flowchart LR
    UI["React 管理后台"] --> API["Go / chi HTTP API"]
    API --> Store["SQLite / MySQL / PostgreSQL"]
    API --> Manager["账号管理器"]
    Manager --> Engine["单账号运行时"]
    Engine --> WS["闲鱼 WebSocket"]
    Engine --> MTOP["闲鱼 MTOP"]
    Engine --> Automation["自动化中心"]
    Automation --> Notify["通知渠道"]
    Engine -. "仅指纹与滑块风控" .-> Chromium["Playwright / Chromium"]
```

核心职责划分：

- `internal/xianyu`：登录协议、Cookie、MTOP、WebSocket 和消息协议
- `internal/engine`：单账号生命周期、消息处理、回复与交付行为
- `internal/automation`：发货、评价赠品、求评价和任务调度
- `internal/browser`：Chromium 指纹读取与滑块验证
- `internal/db`：多数据库访问、敏感字段加密和嵌入式迁移
- `internal/server`：HTTP API、管理端鉴权和前端静态资源

## 页面预览

![Ydisks闲鱼助手预览](https://raw.githubusercontent.com/Christ9038/Ydisks-Xianyu-Helper/main/docs/img/preview_account.png)

![Ydisks闲鱼助手预览](https://raw.githubusercontent.com/Christ9038/Ydisks-Xianyu-Helper/main/docs/img/preview_im.png)


![Ydisks闲鱼助手预览](https://raw.githubusercontent.com/Christ9038/Ydisks-Xianyu-Helper/main/docs/img/preview_auto.png)


## 快速开始

### 方式一：Docker Compose（推荐）

适用于 Linux x86_64、Linux ARM64 和 Apple Silicon。需要 Docker Engine 或
Docker Desktop，并使用 Docker Compose v2。

Docker 部署会使用容器内 Chromium 的原生 Linux 指纹执行官网静默续期；续期是否成功由闲鱼响应决定，不会因宿主系统是 Linux 而跳过。

```bash
git clone https://github.com/Christ9038/Ydisks-Xianyu-Helper.git
cd Ydisks-Xianyu-Helper
cp .env.example .env
```

编辑 `.env`，至少修改以下配置：

```dotenv
POSTGRES_DB=xianyu
POSTGRES_USER=xianyu
POSTGRES_PASSWORD=替换为高强度密码
DATABASE_URL=postgres://xianyu:替换为URL编码后的数据库密码@postgres:5432/xianyu?sslmode=disable
XIANYU_DATA_KEY=替换为长期固定的随机密钥
XIANYU_ADMIN_PASSWORD=替换为管理员密码
```

推荐使用以下命令生成随机值：

```bash
openssl rand -hex 24       # 适合作为 POSTGRES_PASSWORD，连接串无需额外 URL 编码
openssl rand -base64 48    # 适合作为 XIANYU_DATA_KEY
```

配置完成后，只需一条命令即可拉取 GHCR 镜像、启动 PostgreSQL，并在首次部署时自动创建
管理员：

```bash
docker compose up -d
```

浏览器访问 `http://localhost:59188`，使用用户名 `admin` 和 `.env` 中的
`XIANYU_ADMIN_PASSWORD` 登录。

> GHCR 镜像公开时可以匿名拉取。若镜像仍为私有，请先执行
> `docker login ghcr.io -u Christ9038`，并使用具有 `read:packages` 权限的
> Personal Access Token 登录。

### 方式二：从源码运行

环境要求：

- Go 1.26.4 或兼容版本
- Node.js 22 与 npm
- Chromium 运行所需的系统依赖
- 默认可直接使用 SQLite，无需额外数据库服务

构建前端：

```bash
git clone https://github.com/Christ9038/Ydisks-Xianyu-Helper.git
cd Ydisks-Xianyu-Helper
npm --prefix frontend ci
npm --prefix frontend run build
```

前端资源会在构建时通过 Go 的 `//go:embed` 嵌入服务；如果服务已经在运行，构建完成后
需要重启 Go 服务，浏览器硬刷新本身不会替换运行中进程已加载的前端资源。

### 方式三：桌面端安装包

桌面端由两个进程组成：后台 `xianyu-server` 负责 HTTP API、账号运行时和 Chromium；
`xianyu-tray` 只负责 Windows 托盘或 macOS 菜单栏状态、打开管理页面、打开日志目录和服务控制。
Chromium 不打进 Go 二进制，但 Linux、Windows 和 macOS 独立安装包会内置对应架构的
Playwright driver 与 Chromium runtime，安装后无需用户再下载浏览器。

- Windows：安装器注册 `YdisksXianyuHelper` Windows Service，并将托盘控制器加入当前用户登录启动项。安装阶段会为交互式登录用户授予该服务的状态查询、启动和停止权限；安装完成后从托盘启动、停止、重启或退出服务不再触发 UAC，也不会获得修改或删除服务的权限。
- macOS：安装器注册当前登录用户的两个 LaunchAgent，分别运行后台服务和菜单栏控制器。
- Linux：下载对应架构的 tar 包后，以 root 执行 `./install.sh`；安装包已经包含对应架构的
  Playwright driver 与 Chromium，脚本只通过 Playwright 安装系统依赖，不会再下载浏览器。
  脚本创建专用系统用户并安装 systemd unit。安装完成后打开管理页面，首次设置管理员密码。
  卸载时执行 `./uninstall.sh`，
  默认保留 `/var/lib/ydisks-xianyu-helper` 数据。

桌面端安装后的固定位置和服务标识如下：

| 平台 | 后台服务 | 托盘/菜单栏程序 | 数据与日志 |
| --- | --- | --- | --- |
| Windows | `YdisksXianyuHelper` Windows Service | 当前用户登录启动的 `xianyu-tray.exe` | `C:\ProgramData\YdisksXianyuHelper\data`、`C:\ProgramData\YdisksXianyuHelper\logs` |
| macOS | `com.ydisks.xianyu-helper.server` LaunchAgent | `Ydisks闲鱼助手.app` 内的菜单栏程序 | `~/Library/Application Support/YdisksXianyuHelper`、`~/Library/Logs/YdisksXianyuHelper` |
| Linux | `ydisks-xianyu-helper.service` systemd unit | 无桌面托盘 | `/var/lib/ydisks-xianyu-helper`、`/var/log/ydisks-xianyu-helper` |

Windows 和 macOS 托盘启动时会自动启动后台服务，并显示检查中、启动中、运行正常、正在停止
等状态。选择“退出托盘”时，只有确认后台服务已经停止后托盘才会退出；“打开日志目录”可直接
打开日志位置。Windows 安装阶段会为交互式登录用户授予当前服务的查询、启动和停止权限，
安装完成后托盘控制服务不需要重复确认 UAC；修改服务配置或删除服务仍需管理员权限。

托盘菜单中的“退出托盘”会先停止后台服务，再退出托盘程序；“打开日志目录”可直接打开当前平台的日志目录：Windows
为 `C:\ProgramData\YdisksXianyuHelper\logs`，macOS 为
`~/Library/Logs/YdisksXianyuHelper`。Linux 服务日志同时写入
`/var/log/ydisks-xianyu-helper/server.log`，也可以使用
`journalctl -u ydisks-xianyu-helper.service` 查看。

桌面端安装包由 GitHub Actions 的 `codex/desktop-packaging` 分支专用工作流构建：Linux 上传
amd64/arm64 tar 包，Windows 上传安装器，macOS 分别上传 arm64 和 amd64 安装包。Windows
和 macOS 安装包使用工作流中配置的固定签名证书签名；未配置对应 GitHub Secrets 时，签名
步骤会失败，不会生成可分发的安装包。

Linux 安装包的 `install.sh` 必须在与安装包相同架构的 Linux 主机上以 root 执行。安装包已经
包含对应架构的 Playwright driver、Chromium 和 headless shell；安装时只安装 Chromium 所需
系统库，不会从 Debian 仓库重新安装 Chromium。

本地 macOS 打包可直接执行：

```bash
./packaging/macos/prepare-runtime.sh arm64 ./dist/macos/playwright-runtime/arm64
./packaging/macos/build-pkg.sh 0.0.0-local ./dist/macos arm64
```

`build-pkg.sh` 会自动复用 `~/Library/Caches/ms-playwright-go` 和
`~/Library/Caches/ms-playwright` 中与当前 Playwright driver 匹配的 Chromium；缓存不完整时会明确报错，
不会生成安装后无法启动服务的残缺安装包。Intel macOS 使用 `amd64` 参数，并需要本机已有对应 x64 runtime。

桌面端首次启动：

1. 安装对应平台的安装包。macOS 请选择 arm64（Apple Silicon）或 amd64（Intel）版本。
2. 启动后台服务和托盘/菜单栏控制器。
3. 打开托盘菜单中的管理页面，或访问 `http://127.0.0.1:59188`。
4. 如果数据库尚未初始化，页面会要求设置并确认管理员密码；完成后会自动登录，管理员用户名为 `admin`。

Linux 需要先安装并启动 systemd 服务：

```bash
sudo ./install.sh
```

然后访问 `http://127.0.0.1:59188` 完成首次初始化。

Linux、Windows、macOS 和源码运行默认都使用网页初始化：启动服务后打开
`http://127.0.0.1:59188`，在“首次设置管理员密码”页面输入并确认不少于 8 个字符的密码，
系统会创建 `admin` 并自动登录。只有 Docker Compose 使用 `.env` 中的
`XIANYU_ADMIN_PASSWORD` 做非交互初始化；CLI 方式仅用于无浏览器、自动化部署或重置密码。

对于无浏览器环境、自动化部署或需要重置管理员密码的运维场景，仍可使用 CLI 初始化：

```bash
mkdir -p data
go run ./cmd/server \
  -init-admin \
  -db data/xianyu_data.db \
  -admin-password '请替换为管理员密码'
```

启动服务：

```bash
go run ./cmd/server -db data/xianyu_data.db -addr :59188
```

启动完成后访问 `http://localhost:59188`。如果数据库尚未初始化，直接在页面填写并确认
管理员密码即可，不需要知道数据库文件路径，也不需要先执行 CLI 命令。

如当前环境无法安装 Chromium，可以使用 `-no-browser` 启动：

```bash
go run ./cmd/server -db data/xianyu_data.db -addr :59188 -no-browser
```

此模式仍可运行管理后台，但浏览器指纹读取和滑块风控处理不可用，不建议用于需要登录
和长期运行账号的生产环境。

## 初次使用

1. 使用管理员账号登录管理后台。
2. 进入“账号管理”，通过闲鱼 App 扫描二维码完成账号授权。
3. 如平台触发风控，根据页面提示在手机端完成人脸或安全验证。
4. 等待账号状态变为在线，再同步商品和订单。
5. 在“卡密库存”创建交付内容。
6. 在“自动化规则”将闲鱼商品与卡密、规格和触发条件关联。
7. 在“在线聊天”按账号查看历史会话、发送文本/表情/图片；官方交易卡片和闲小蜜消息会以系统消息显示，不会进入普通用户回复链。
8. 在“账号管理 → 自动评价与每日擦亮”按账号配置统一评价文案、评价开关和擦亮时间；确认测试账号行为后再启用。
9. 按需配置关键词回复、默认回复、AI 回复和通知渠道。

扫码登录是推荐方式。密码登录适合已有明确需求的环境，但更容易触发平台安全校验。
无论使用哪种方式，都不要把 Cookie、密码、二维码或人脸验证地址分享给不可信第三方。

## 配置说明

配置分为三层：进程环境变量、命令行参数和管理后台设置。数据库连接优先级为：

```text
DATABASE_URL > -db-url > -db
```

### 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DATABASE_URL` | 空 | 数据库连接 URL；生产环境推荐 PostgreSQL |
| `XIANYU_DATA_KEY` | 空 | 敏感字段加密主密钥；生产环境必须固定并备份 |
| `XIANYU_ADMIN_PASSWORD` | 空 | Docker Compose 非交互初始化或重置管理员时使用；源码和桌面端可通过首次打开页面设置密码 |
| `XIANYU_UPLOAD_DIR` | `data/uploads` | 批量铺货上传文件与临时资源目录 |
| `LOG_LEVEL` | 系统设置或 `info` | `debug`、`info`、`warn`、`error` |
| `LOG_FORMAT` | 系统设置或 `text` | `text` 或 `json` |
| `BROWSER_HEADLESS` | 按账号设置；Docker 为 `true` | 强制启用或关闭无头 Chromium |
| `PLAYWRIGHT_BROWSERS_PATH` | 自动发现；Docker 为 `/ms-playwright` | Playwright Chromium runtime 目录 |
| `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` | 自动发现 | 仅在需要强制使用外部 Chromium 时指定 |
| `PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD` | 源码 `false` / Docker `true` | 跳过下载 |
| `TZ` | 系统时区；Docker 为 `Asia/Shanghai` | 容器和日志时区 |

Docker Compose 还支持：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `COMPOSE_PROJECT_NAME` | `ydisks-xianyu-helper` | Compose 项目名及命名卷前缀 |
| `POSTGRES_IMAGE` | `postgres:17-trixie` | PostgreSQL 镜像 |
| `POSTGRES_DB` | 必填 | 数据库名 |
| `POSTGRES_USER` | 必填 | 数据库用户 |
| `POSTGRES_PASSWORD` | 必填 | 数据库密码 |
| `XIANYU_IMAGE` | `ghcr.io/christ9038/ydisks-xianyu-helper:latest` | 应用镜像与标签 |
| `XIANYU_BIND_ADDRESS` | `0.0.0.0` | 应用在宿主机上的绑定地址 |
| `XIANYU_HTTP_PORT` | `59188` | 应用在宿主机上的端口 |

默认 `compose.yml` 直接拉取 GHCR 的 `:latest` 多架构镜像；如需固定版本，请将
`XIANYU_IMAGE` 改为 `:main`、版本号或 `:sha-<提交号>` 标签。

`XIANYU_DATA_KEY` 用于加密 Cookie、账号密码、设备令牌、访问令牌、AI/SMTP 密钥和
通知凭证。启用后，服务会自动升级历史明文数据。密钥丢失或更换后，已有加密数据将
无法解密，因此必须与数据库备份一起保存。

### 命令行参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-db` | `data/xianyu_data.db` | SQLite 数据库路径 |
| `-db-url` | 空 | `sqlite://`、`mysql://` 或 `postgres://` 连接 URL |
| `-addr` | `:59188` | HTTP 监听地址 |
| `-web` | 内嵌前端 | 外部前端静态资源目录，目录内需包含 `index.html` |
| `-secure` | `false` | 为管理端 Cookie 添加 `Secure` 属性，HTTPS 部署应启用 |
| `-no-browser` | `false` | 禁用 Chromium 指纹读取和滑块处理 |
| `-log-level` | 环境变量或系统设置 | 覆盖日志等级 |
| `-log-format` | 环境变量或系统设置 | 覆盖日志格式 |
| `-v` | `false` | 启用调试日志，等价于未显式配置时使用 debug |
| `-init-admin` | `false` | 初始化或重置 `admin` 后退出 |
| `-ensure-admin` | `false` | 仅在 `admin` 不存在时初始化；已存在时不重置密码 |
| `-admin-email` | `admin@example.com` | 初始化管理员邮箱 |
| `-admin-password` | 空 | 初始化管理员密码 |

### 数据库连接示例

```bash
# SQLite
DATABASE_URL="sqlite://data/xianyu_data.db" ./xianyu-server

# MySQL；应用会强制补齐 multiStatements=true 和 clientFoundRows=true
DATABASE_URL="mysql://user:pass@tcp(127.0.0.1:3306)/xianyu" ./xianyu-server

# PostgreSQL
DATABASE_URL="postgres://user:pass@127.0.0.1:5432/xianyu?sslmode=disable" ./xianyu-server
```

每次启动都会自动执行对应数据库方言的嵌入式 Goose 迁移。生产数据库升级前仍建议
先完成备份。

### 管理后台配置

以下配置通过 Web 管理后台维护并保存在数据库中：

- **账号设置**：启停状态、备注、通知渠道绑定和单账号 AI 回复策略
- **账号自动任务**：自动评价开关、统一好评文案（最多 500 个字符）、每日擦亮开关、北京时间执行时间和最近执行记录
- **自动化规则**：触发条件、商品、规格、卡密组、发送数量、延迟和后续动作
- **回复规则**：关键词、文本或图片回复、默认回复及回复次数限制
- **AI 设置**：OpenAI 兼容 API 地址、API Key、模型和全局提示词
- **议价策略**：最大折扣比例、最大折扣金额和最多议价轮次
- **通知设置**：Bark、钉钉、飞书、企业微信、Telegram、邮件和 Webhook
- **日志设置**：日志等级、输出格式和续期日志保留天数
- **远程滑块服务**：服务地址、密钥及是否允许传递 Cookie；默认不传递 Cookie
- **管理凭据**：管理员用户名、密码和邮箱

AI Base URL 支持 OpenAI 兼容接口，可连接 OpenAI、通义千问、Ollama、vLLM、
LocalAI 或兼容网关。只有在明确了解数据流向时才应向外部 AI 或远程验证服务传递
消息内容与账号数据。

## Docker 部署

### 镜像与架构

GitHub Actions 会将同一标签发布为多架构镜像：

```text
ghcr.io/christ9038/ydisks-xianyu-helper:latest
├── linux/amd64
└── linux/arm64
```

Docker 会根据宿主机 CPU 自动选择镜像。x86_64 Linux 拉取 amd64，Apple M 系列和
ARM64 Linux 拉取 arm64，不需要手动设置 `platform`。

标签规则：

| Git 引用 | 镜像标签 |
| --- | --- |
| `main` 分支 | `main`、`latest`、`sha-<完整提交号>` |
| `codex/desktop-packaging` 分支 | `alpha`、`desktop-packaging`、`sha-<完整提交号>` |
| `v1.2.3` 标签 | `1.2.3`、`1.2`、`sha-<完整提交号>` |

当前 Docker 发布工作流会在 `main` 分支发布正式镜像，并在
`codex/desktop-packaging` 分支发布 `alpha` 标签用于验收；其他开发分支不会自动生成
GHCR 标签。

生产环境建议使用明确的版本标签或 SHA 标签，避免上游更新导致未经验证的自动升级。

### Compose 服务

根目录的 [`compose.yml`](compose.yml) 是默认生产部署文件：它只拉取 GHCR 镜像，完全不
包含 `build` 配置，因此服务器不需要安装 Go、Node.js 或项目源码构建依赖。完成一次
`.env` 配置后，始终使用：

```bash
docker compose up -d
```

它包含两个服务：

| 服务 | 用途 |
| --- | --- |
| `postgres` | PostgreSQL 17 数据库，仅在 Compose 内部网络开放 5432 |
| `app` | Ydisks闲鱼助手主服务、前端和 Chromium |

`app` 会在首次启动时自动创建 `admin`；如果管理员已存在，后续 `up`、重启或升级均不会
修改其密码。

`docker-compose.debian13-postgres17.yml` 保留为**源码构建版**，用于本地修改 Dockerfile
或需要构建本地镜像的场景；生产服务器无需使用该文件。

持久化卷：

| 卷 | 内容 |
| --- | --- |
| `postgres_data` | PostgreSQL 数据文件 |
| `app_data` | 上传文件、批量铺货资源和应用数据 |
| `browser_data` | Chromium 持久化配置和验证上下文 |

### 完整部署步骤

```bash
git clone https://github.com/Christ9038/Ydisks-Xianyu-Helper.git
cd Ydisks-Xianyu-Helper
cp .env.example .env
```

完成 `.env` 配置后执行：

```bash
docker compose up -d
```

检查运行状态：

```bash
docker compose ps
curl -fsS http://127.0.0.1:59188/health
docker compose logs --tail=200 app
```

健康接口正常时返回：

```json
{"database":"ok","status":"ok"}
```

### 更新镜像

```bash
docker compose pull app
docker compose up -d
docker compose logs --tail=100 app
```

更新前请先备份数据库，并确认 `.env` 中的 `XIANYU_DATA_KEY` 未发生变化。

### 备份 PostgreSQL

```bash
set -a
. ./.env
set +a

docker compose exec -T postgres \
  pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  > "xianyu-$(date +%Y%m%d-%H%M%S).sql"
```

恢复前请先停止应用，并在独立环境验证备份文件：

```bash
docker compose stop app
docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" < xianyu-backup.sql
```

同时备份 `.env` 中的 `XIANYU_DATA_KEY`。不要把包含真实密码和密钥的 `.env` 提交到
Git；项目已默认忽略该文件。

### 停止服务

```bash
# 停止并保留数据卷
docker compose down

# 查看数据卷
docker volume ls --filter name=xianyu
```

不要在生产环境随意执行 `down -v`，该参数会删除 Compose 管理的数据库和应用数据卷。

### HTTPS 与反向代理

生产环境建议使用 Caddy、Nginx、Traefik 或云负载均衡器终止 TLS，并避免直接向公网
暴露管理端口。启用 HTTPS 后，应为应用命令增加 `-secure`，确保管理端会话 Cookie
仅通过 HTTPS 发送。例如可创建一个 Compose override：

```yaml
services:
  app:
    command: ["/app/xianyu-server", "-addr", ":59188", "-secure"]
```

## 开发指南

### 项目结构

```text
.
├── cmd/
│   ├── server/           # 主服务与管理员初始化入口
│   ├── browser-install/   # Playwright Chromium 安装辅助程序
│   ├── tray/              # Windows 托盘/macOS 菜单栏控制器
│   ├── init-admin/       # 交互式管理员初始化工具
│   ├── dbverify/         # 多数据库迁移和 CRUD 验证
│   └── dbseed/           # Docker 功能测试数据种子
├── internal/
│   ├── account/          # 多账号监督与生命周期
│   ├── adapter/          # 业务模块接线层
│   ├── automation/       # 自动化中心与调度器
│   ├── browser/          # Chromium 指纹和滑块验证
│   ├── db/               # 数据访问、加密与迁移
│   ├── engine/           # 单账号消息和交付运行时
│   ├── notify/           # 通知渠道
│   ├── renewal/          # 登录态续期调度
│   ├── server/           # HTTP API 与 SPA 服务
│   └── xianyu/           # MTOP、WebSocket、登录和协议实现
├── frontend/             # React、TypeScript、Vite 前端
├── docs/                 # 专题文档和行为规范
├── packaging/             # Linux systemd、Windows Inno Setup、macOS pkg
├── scripts/              # Docker 与回归测试脚本
└── Dockerfile.debian13   # 多阶段生产镜像
```

### 本地开发

启动后端：

```bash
go run ./cmd/server -db data/xianyu_data.db -addr :59188
```

启动前端开发服务器：

```bash
npm --prefix frontend ci
npm --prefix frontend run dev
```

访问 `http://localhost:3000`。Vite 会将 API 请求代理到 `http://localhost:59188`。

构建嵌入式前端和 Go 二进制：

```bash
make frontend
make build
```

### 质量检查

```bash
make fmt       # 格式化 Go 代码
make vet       # go vet
make lint      # golangci-lint
make test      # 全部 Go 单元测试
make cover     # 覆盖率报告
make check     # fmt + vet + lint + test

npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run build
```

完整 Docker 功能回归会检查前端、Go `-race`、SQLite、MySQL 8.4、PostgreSQL 17、
API 功能和容器重启持久化：

```bash
./scripts/docker-full-test.sh
```

验证指定数据库的迁移与核心 CRUD：

```bash
go run ./cmd/dbverify "sqlite:///tmp/xianyu-verify.db"
go run ./cmd/dbverify "mysql://root:pass@tcp(127.0.0.1:3306)/xianyu"
go run ./cmd/dbverify "postgres://user:pass@127.0.0.1:5432/xianyu"
```

## 常见问题

### 首次打开页面如何初始化管理员

启动服务后访问管理页面。如果数据库中还没有管理员，页面会显示“首次设置管理员密码”，
填写两次不少于 8 个字符的密码并提交即可。初始化成功后会自动登录，默认管理员用户名为
`admin`。

Docker Compose 使用非交互初始化，因此必须在 `.env` 中设置非空的
`XIANYU_ADMIN_PASSWORD`，然后执行：

```bash
docker compose up -d
```

无浏览器、自动化部署或需要重置管理员密码时可执行：

```bash
go run ./cmd/server -init-admin -db data/xianyu_data.db -admin-password '新密码'
```

### PostgreSQL 连接失败

- 确认 `.env` 中 `POSTGRES_DB`、`POSTGRES_USER`、`POSTGRES_PASSWORD` 与
  `DATABASE_URL` 完全一致。
- Compose 内部的数据库主机名必须写 `postgres`，不能写 `localhost`。
- 密码包含 `@`、`:`、`/`、`#` 等字符时，需要在 `DATABASE_URL` 中进行 URL 编码。
- 使用 `docker compose ... ps` 确认 PostgreSQL 健康检查已通过。

### Chromium 或 Playwright 初始化失败

- Docker 部署优先使用项目提供的镜像，镜像在构建阶段已下载 Chromium，并包含运行依赖；
  不依赖 Debian 仓库中的 `chromium` 包。
- 独立安装包在 CI 构建阶段下载对应架构的 Playwright driver 和 Chromium；Linux 安装时只
  安装系统库，不需要再次下载浏览器。
- 源码运行时确认系统允许下载 Playwright 驱动和 Chromium。
- 自带 Chromium 时设置 `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH`，并按需设置
  `PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1`。
- 容器内需要足够的共享内存与可写的 `browser_data` 卷。

### 账号掉线或需要安全验证

- 查看“账号管理”中的状态和续期日志。
- 根据页面二维码在闲鱼 App 完成人脸或安全验证。
- 检查服务器时间、时区和外网连接是否正常。
- 不要同时在多个实例中运行同一个闲鱼账号，避免凭证相互覆盖。

### 修改 `XIANYU_DATA_KEY` 后无法读取凭证

这是预期的安全行为。请恢复写入这些数据时使用的原密钥。无法恢复原密钥时，只能重新
登录账号并重新录入受保护的密钥；不要通过清空或绕过解密检查继续运行生产数据库。

## 安全建议

- 仅通过 HTTPS 暴露管理后台，并限制可信来源访问。
- 为管理员、数据库和 GHCR 分别使用不同的高强度密码。
- 固定并离线备份 `XIANYU_DATA_KEY`，不要在多套环境之间随意复用。
- 不要提交 `.env`、数据库文件、Cookie、二维码、日志或浏览器数据目录。
- 定期备份 PostgreSQL，并实际验证恢复流程。
- 使用版本或 SHA 镜像标签部署生产环境。
- 仅向可信的 AI、SMTP、Webhook 和远程滑块服务发送数据。
- 发现账号异常时先停用账号，再检查登录审计和续期日志。

## 贡献

欢迎通过 Issue 报告可复现的问题，或通过 Pull Request 提交改进。提交前请：

1. 将不同主题拆分为独立提交。
2. 为协议、数据库和关键业务行为补充测试。
3. 运行 `make check` 和前端测试。
4. 不要提交真实账号、Cookie、订单、卡密、密钥或其他敏感数据。
5. 不要在未说明原因和验证证据的情况下修改风控与滑块行为。

提交安全问题时，请避免在公开 Issue 中附带真实凭证或用户数据。

## 开源协议

本项目采用 [Apache License 2.0](LICENSE) 开源，版权所有 © 2026 Christ9038。

完整许可条款见 [LICENSE](LICENSE)，
原项目名称、版权与署名声明见 [NOTICE](NOTICE)。分发原始或衍生作品时，请保留这两个
文件中的适用声明，并在修改过的文件中显著标注修改。

## 免责声明

本项目仅提供技术研究和自托管管理能力，不保证对闲鱼接口变化、账号风控策略、业务
连续性或数据完整性的长期兼容。因使用本项目造成的账号限制、交易纠纷、数据损失或
其他直接与间接损失，应由使用者自行承担。请勿将本项目用于垃圾消息、欺诈、未授权
访问、规避平台限制或任何违法违规活动。
