#!/bin/sh
set -eu

# 将本机 Playwright 缓存整理成 macOS 安装包使用的固定 runtime 目录。
# CI 通过 browser-install 下载 runtime；本地打包直接复用同一版本的缓存，
# 不需要再次访问 npm 或 Chromium 下载地址。

ARCH="${1:?architecture is required (arm64 or amd64)}"
DEST_DIR="${2:?runtime destination is required}"

case "$ARCH" in
  arm64) PLATFORM_ARCH="arm64" ;;
  amd64) PLATFORM_ARCH="x64" ;;
  *) echo "不支持的 macOS 架构：$ARCH" >&2; exit 1 ;;
esac

PLAYWRIGHT_GO_CACHE="${PLAYWRIGHT_GO_CACHE:-${HOME:?}/Library/Caches/ms-playwright-go}"
PLAYWRIGHT_BROWSER_CACHE="${PLAYWRIGHT_BROWSER_CACHE:-${HOME:?}/Library/Caches/ms-playwright}"

if [ ! -d "$PLAYWRIGHT_GO_CACHE" ]; then
  echo "找不到 Playwright Go 缓存：$PLAYWRIGHT_GO_CACHE" >&2
  echo "请先运行 browser-install，或设置 PLAYWRIGHT_GO_CACHE。" >&2
  exit 1
fi
if [ ! -d "$PLAYWRIGHT_BROWSER_CACHE" ]; then
  echo "找不到 Playwright 浏览器缓存：$PLAYWRIGHT_BROWSER_CACHE" >&2
  echo "请先运行 browser-install，或设置 PLAYWRIGHT_BROWSER_CACHE。" >&2
  exit 1
fi

DRIVER_DIR=""
for candidate in "$PLAYWRIGHT_GO_CACHE"/[0-9]*; do
  if [ -d "$candidate/package" ] && [ -x "$candidate/node" ]; then
    DRIVER_DIR="$candidate"
  fi
done
if [ -z "$DRIVER_DIR" ]; then
  echo "Playwright Go 缓存中没有可用 driver：$PLAYWRIGHT_GO_CACHE" >&2
  exit 1
fi

BROWSER_DIR=""
HEADLESS_DIR=""
for candidate in "$PLAYWRIGHT_BROWSER_CACHE"/chromium-[0-9]*; do
  [ -d "$candidate" ] || continue
  revision="${candidate##*/chromium-}"
  chrome="$candidate/chrome-mac-$PLATFORM_ARCH/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"
  headless="$PLAYWRIGHT_BROWSER_CACHE/chromium_headless_shell-$revision/chrome-headless-shell-mac-$PLATFORM_ARCH/chrome-headless-shell"
  if [ -f "$chrome" ] && [ -f "$headless" ]; then
    BROWSER_DIR="$candidate"
    HEADLESS_DIR="$PLAYWRIGHT_BROWSER_CACHE/chromium_headless_shell-$revision"
  fi
done
if [ -z "$BROWSER_DIR" ]; then
  echo "Playwright 浏览器缓存中没有 $ARCH 架构的 Chromium/headless shell：$PLAYWRIGHT_BROWSER_CACHE" >&2
  exit 1
fi

revision="${BROWSER_DIR##*/chromium-}"
if ! grep -q '"revision": "'"$revision"'"' "$DRIVER_DIR/package/browsers.json"; then
  echo "Playwright driver 与 Chromium revision 不匹配：driver=$DRIVER_DIR，browser=$revision" >&2
  exit 1
fi

rm -rf "$DEST_DIR/playwright-driver" "$DEST_DIR/playwright-browsers"
mkdir -p "$DEST_DIR"
cp -R "$DRIVER_DIR" "$DEST_DIR/playwright-driver"
mkdir -p "$DEST_DIR/playwright-browsers"
cp -R "$BROWSER_DIR" "$DEST_DIR/playwright-browsers/"
cp -R "$HEADLESS_DIR" "$DEST_DIR/playwright-browsers/"

echo "已从本机缓存准备 macOS $ARCH Playwright runtime：revision=$revision"
