#!/bin/sh
set -eu

VERSION="${1:?version is required}"
DIST_DIR="${2:?dist directory is required}"
ARCH="${3:?architecture is required (arm64 or amd64)}"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
PROJECT_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)"
BIN_DIR="$DIST_DIR/$ARCH"
ROOT_DIR="$DIST_DIR/pkgroot-$ARCH"
APP_DIR="$ROOT_DIR/Applications/Ydisks闲鱼助手"
APP="$APP_DIR/Ydisks闲鱼助手.app"
PACKAGE_PATH="$DIST_DIR/Ydisks-Xianyu-Helper-$VERSION-$ARCH.pkg"
UNSIGNED_PACKAGE_PATH="$DIST_DIR/.Ydisks-Xianyu-Helper-$VERSION-$ARCH.unsigned.pkg"

case "$ARCH" in
  arm64|amd64) ;;
  *) echo "不支持的 macOS 架构：$ARCH" >&2; exit 1 ;;
esac

if [ ! -x "$BIN_DIR/xianyu-server" ] || [ ! -x "$BIN_DIR/xianyu-tray" ]; then
  echo "缺少 $ARCH 架构的桌面二进制：$BIN_DIR" >&2
  exit 1
fi
if [ ! -d "$DIST_DIR/playwright-runtime/$ARCH/playwright-driver" ] || \
   [ ! -d "$DIST_DIR/playwright-runtime/$ARCH/playwright-browsers" ]; then
  echo "缺少 $ARCH 架构的 Playwright runtime：$DIST_DIR/playwright-runtime/$ARCH" >&2
  exit 1
fi

rm -rf "$ROOT_DIR"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Helpers" "$APP/Contents/Resources"
cp "$BIN_DIR/xianyu-server" "$APP/Contents/Helpers/xianyu-server"
cp "$BIN_DIR/xianyu-tray" "$APP/Contents/MacOS/Ydisks闲鱼助手"
cp "$SCRIPT_DIR/uninstall.command" "$APP_DIR/卸载 Ydisks闲鱼助手.command"
cp "$SCRIPT_DIR/com.ydisks.xianyu-helper.server.plist.template" "$APP/Contents/Resources/"
cp "$SCRIPT_DIR/com.ydisks.xianyu-helper.tray.plist.template" "$APP/Contents/Resources/"
mkdir -p "$APP/Contents/Resources/playwright-runtime/$ARCH"
cp -R "$DIST_DIR/playwright-runtime/$ARCH/." "$APP/Contents/Resources/playwright-runtime/$ARCH/"
cp "$PROJECT_ROOT/icon/macos/Assets.car" "$APP/Contents/Resources/Assets.car"
cp "$PROJECT_ROOT/icon/macos/icon.icns" "$APP/Contents/Resources/icon.icns"
sed "s/__VERSION__/$VERSION/g" "$SCRIPT_DIR/Info.plist" > "$APP/Contents/Info.plist"
chmod 0755 "$APP/Contents/MacOS/Ydisks闲鱼助手" "$APP/Contents/Helpers/xianyu-server" "$APP_DIR/卸载 Ydisks闲鱼助手.command"

if [ -n "${MACOS_SIGNING_IDENTITY:-}" ]; then
  sign_code() {
    target="$1"
    if [ -n "${MACOS_SIGNING_KEYCHAIN:-}" ]; then
      codesign --force --sign "$MACOS_SIGNING_IDENTITY" \
        --keychain "$MACOS_SIGNING_KEYCHAIN" --timestamp=none "$target"
    else
      codesign --force --sign "$MACOS_SIGNING_IDENTITY" \
        --timestamp=none "$target"
    fi
  }

  sign_nested_bundle() {
    nested_bundle="$1"
    echo "Signing nested bundle: $nested_bundle"
    if [ -n "${MACOS_SIGNING_KEYCHAIN:-}" ]; then
      codesign --force --deep --sign "$MACOS_SIGNING_IDENTITY" \
        --keychain "$MACOS_SIGNING_KEYCHAIN" --timestamp=none "$nested_bundle"
    else
      codesign --force --deep --sign "$MACOS_SIGNING_IDENTITY" \
        --timestamp=none "$nested_bundle"
    fi
  }

  # Playwright runtime 中包含 Node 和 Chromium.app，必须在 App 签名之前完成签名。
  # 先签每个 Mach-O 文件，再按目录深度从内到外签 bundle。Chromium 的
  # .app 内还嵌套了 .framework/.xpc 和其他 helper .app；必须先签 Mach-O，
  # 再按目录深度对每个 bundle 递归签名，最后才签外层应用。
  signing_list="$(mktemp)"
  trap 'rm -f "$signing_list"' EXIT

  find "$APP/Contents/Resources/playwright-runtime" -type f -print > "$signing_list"
  while IFS= read -r executable; do
    if file "$executable" | grep -q 'Mach-O'; then
      sign_code "$executable"
    fi
    # `set -e` is not reliable for a while loop in a pipeline on macOS /bin/sh.
    # Exit explicitly so a failed child signature reaches the parent script.
  done < "$signing_list"

  find "$APP/Contents/Resources/playwright-runtime" -depth -type d \
    \( -name '*.framework' -o -name '*.xpc' -o -name '*.app' \) -print > "$signing_list"
  while IFS= read -r nested_bundle; do
    sign_nested_bundle "$nested_bundle"
  done < "$signing_list"

  # macOS 代码签名必须从内部组件开始，最后再签名 App 包本身。
  sign_code "$APP/Contents/Helpers/xianyu-server"
  sign_code "$APP/Contents/MacOS/Ydisks闲鱼助手"
  sign_code "$APP"
  codesign --verify --deep --strict --verbose=2 "$APP"
fi

if [ -n "${MACOS_SIGNING_IDENTITY:-}" ] && [ -z "${MACOS_INSTALLER_SIGNING_IDENTITY:-}" ]; then
  echo 'MACOS_INSTALLER_SIGNING_IDENTITY 未设置，不能生成已签名 macOS 安装包' >&2
  exit 1
fi

rm -f "$PACKAGE_PATH" "$UNSIGNED_PACKAGE_PATH"

pkgbuild \
  --root "$ROOT_DIR" \
  --component-plist "$SCRIPT_DIR/component.plist" \
  --scripts "$SCRIPT_DIR/scripts" \
  --identifier com.ydisks.xianyu-helper \
  --version "$VERSION" \
  --install-location / \
  "$UNSIGNED_PACKAGE_PATH"

if [ -n "${MACOS_INSTALLER_SIGNING_IDENTITY:-}" ]; then
  if [ -n "${MACOS_SIGNING_KEYCHAIN:-}" ]; then
    productsign --sign "$MACOS_INSTALLER_SIGNING_IDENTITY" \
      --keychain "$MACOS_SIGNING_KEYCHAIN" \
      "$UNSIGNED_PACKAGE_PATH" "$PACKAGE_PATH"
  else
    productsign --sign "$MACOS_INSTALLER_SIGNING_IDENTITY" \
      "$UNSIGNED_PACKAGE_PATH" "$PACKAGE_PATH"
  fi
  pkgutil --check-signature "$PACKAGE_PATH"
  rm -f "$UNSIGNED_PACKAGE_PATH"
else
  mv "$UNSIGNED_PACKAGE_PATH" "$PACKAGE_PATH"
fi
