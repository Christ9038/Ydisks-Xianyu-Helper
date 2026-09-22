# Docker 自更新

Docker 自更新由宿主机上的 `ydisks-updater` daemon 执行。应用容器只挂载只读的 Unix Socket，永远不挂载 `/var/run/docker.sock`；Socket 协议只允许查询状态、检查版本和更新到官方最新正式版三个固定操作。

## 支持范围

第一版只自动更新以下部署：

- SQLite 数据库；
- `/app/data` 绑定到部署目录下的 `data`；
- 官方稳定镜像或可被检查的稳定部署；
- 数据库备份、完整性检查和候选版本迁移预检全部通过。

PostgreSQL、MySQL、Docker 命名卷、开发版和自定义镜像仍可查看当前版本，但不会显示可执行的一键更新按钮。这样不会把不同数据库迁移路径或用户定制镜像误切换成官方版本。

## 安装宿主机更新器

从正式 Release 下载当前架构的 `ydisks-updater-linux-amd64` 或 `ydisks-updater-linux-arm64`，审阅文件后安装到固定路径：

```bash
sudo install -o root -g root -m 0755 ydisks-updater-linux-arm64 /usr/local/libexec/ydisks-updater
sudo install -m 0644 deploy/systemd/ydisks-updater.service /etc/systemd/system/ydisks-updater.service
sudo systemctl daemon-reload
sudo systemctl enable --now ydisks-updater.service
```

在已存在的 SQLite Compose 部署上启用应用 Socket 覆盖：

```bash
docker compose -f compose.yml -f deploy/docker/compose.updater.yml up -d app
```

systemd 会以 `root:docker` 自动创建 `/run/ydisks-xianyu-helper`，更新器在其中监听 `updater.sock`。应用容器通过覆盖文件以只读方式挂载该目录，并从 `XIANYU_UPDATE_SOCKET` 读取固定容器内路径。

## 更新流程

管理后台“系统设置”中的“系统更新”面板先调用检查接口。点击更新后，宿主机会：

1. 重新读取 GitHub Release 的 `docker-manifest.json`；
2. 只拉取不可变 OCI manifest digest；
3. 创建 SQLite 在线副本并执行完整性、外键检查；
4. 使用该副本运行候选镜像迁移预检；
5. 短暂停止 `app`，再创建包含停止前最后写入的最终回滚备份；
6. 只重建 `app` 服务并等待 `/health`、版本和提交号一致；
7. 失败时恢复最终数据库备份、旧镜像和更新前 Compose 环境，并报告“已回滚”。

更新过程会短暂重启应用容器和账号连接，不执行 `docker compose down -v`，也不会重启 Docker、SSH、Cloudflare 或其他服务。
