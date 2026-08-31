# Ydisks闲鱼助手 Wiki

本 Wiki 面向管理后台操作人员。先完成“部署与数据库”和“账号接入”，再配置库存、商品、发货模板、
在线聊天、账号自动任务、交易自动化与 AI。

## 推荐上线路径

1. 选择部署方式：Docker Compose、Linux 安装包、Windows 安装包、macOS 安装包或源码运行。
2. Docker Compose 设置 `POSTGRES_*`、`DATABASE_URL`、`XIANYU_DATA_KEY` 和
   `XIANYU_ADMIN_PASSWORD`；其他方式启动服务后打开管理页面，在首次初始化页面填写密码。
3. 设置并备份 `XIANYU_DATA_KEY`；默认管理员用户名为 `admin`。
4. 在“账号管理”扫码接入账号，确认账号为启用且在线状态。
5. 在“卡密库存”建立可交付内容。
6. 同步已有商品，或通过“商品管理”发布商品。
7. 可选：在“发货模板”建立多条顺序消息，并在付款发货或评价赠品规则中绑定。
8. 在“在线聊天”确认账号会话和历史消息正常，必要时发送测试文本或图片。
9. 在“账号自动任务”配置自动评价和每日擦亮；在“自动化规则”建立改价、付款发货、评价赠品或求评价规则。
10. 可选：在“系统与 AI”配置模型，再为每个账号启用 AI。
11. 建立通知渠道并绑定到账号；用测试通知验证。
12. 用小额测试订单验证“卡密发送 → 确认发货”全过程后，再开放正式商品。

桌面安装包的管理地址是 `http://127.0.0.1:59188`。Windows 和 macOS 安装包自带后台服务、托盘/菜单栏
控制器以及匹配架构的 Playwright driver、Chromium；Linux 安装包自带相同的浏览器 runtime，
安装脚本只补齐系统依赖。桌面托盘退出前会先停止后台服务。

管理后台侧边栏底部会显示当前运行版本和短提交号。源码运行默认显示 `dev`/`unknown`；正式安装包和
Docker 镜像会显示构建时注入的正式版本与提交信息。

Windows 安装包注册 `YdisksXianyuHelper` 服务；macOS 安装包分别提供 Apple Silicon（arm64）和
Intel（amd64）版本，并注册 `com.ydisks.xianyu-helper.server` 与
`com.ydisks.xianyu-helper.tray` 两个 LaunchAgent。Linux 安装包按 amd64/arm64 分发，执行
`sudo ./install.sh` 后注册 `ydisks-xianyu-helper.service`。三种独立安装包都在构建阶段准备匹配架构
的 Chromium，不依赖 Debian 仓库版本。

## 先读这些页面

- [部署与数据库](部署与数据库)
- [账号、库存与商品](账号、库存与商品)
- [发货模板](账号、库存与商品#3-发货模板)
- [在线聊天与账号自动任务](在线聊天与账号自动任务)
- [批量铺货](批量铺货)
- [自动化发货与回复](自动化发货与回复)
- [AI、通知与运维](AI、通知与运维)

## 关键安全规则

- 不要提交 `.env`、数据库备份、Cookie、API Key 或通知 Webhook 地址。
- `XIANYU_DATA_KEY` 用于加密 Cookie、密码、AI/SMTP 和通知凭据；换密钥会导致旧数据无法解密。务必和数据库一同离线备份。
- 自动化必须先用测试商品、测试卡密和小额订单验证。库存耗尽、账号掉线或风控时，不应假设发货已完成。
- 本项目为非官方工具。请确保使用方式符合平台规则、当地法律及账号授权范围。

## GitHub Wiki 同步

这些 Markdown 文件位于仓库 `docs/wiki/`，是 Wiki 的唯一维护来源；文件名即页面名，`_Sidebar.md` 是
Wiki 导航。`main` 分支发生变更后，`.github/workflows/sync-wiki.yml` 会自动把这些文件镜像到 GitHub
Wiki；也可以通过 GitHub Actions 手动运行该工作流。仓库内保留副本，便于在 Pull Request 中审阅和版本化维护，
不建议直接在 GitHub Wiki 仓库中维护内容。

项目 Pages 站点使用 `main` 分支的 `/docs` 目录发布，域名由 `docs/CNAME` 中的配置决定；修改首页或图片资源
后，合并到 `main` 即可由 GitHub Pages 发布。Pages 发布与 GitHub Wiki 同步是两个独立流程。
