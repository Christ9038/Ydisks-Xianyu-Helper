#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="ydisks-xianyu-helper"
SERVICE_NAME="$APP_NAME.service"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "请使用 root 运行：sudo $0" >&2
  exit 1
fi

systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
rm -f "/etc/systemd/system/$SERVICE_NAME"
systemctl daemon-reload
rm -rf "/opt/$APP_NAME"
echo "程序和服务已移除；数据仍保留在 /var/lib/$APP_NAME。"
