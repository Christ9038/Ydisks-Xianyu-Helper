#!/bin/sh
set -eu

VERSION="${1:?version is required}"
DIST_DIR="${2:?dist directory is required}"
ROOT_DIR="$DIST_DIR/pkgroot"
APP="$ROOT_DIR/Applications/Ydisks Xianyu Helper.app"
PACKAGE_PATH="$DIST_DIR/Ydisks-Xianyu-Helper-$VERSION.pkg"
UNSIGNED_PACKAGE_PATH="$DIST_DIR/.Ydisks-Xianyu-Helper-$VERSION.unsigned.pkg"

rm -rf "$ROOT_DIR"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Helpers" "$APP/Contents/Resources"
cp "$DIST_DIR/xianyu-server" "$APP/Contents/Helpers/xianyu-server"
cp "$DIST_DIR/browser-install" "$APP/Contents/Helpers/browser-install"
cp "$DIST_DIR/xianyu-tray" "$APP/Contents/MacOS/xianyu-tray"
cp "$(dirname "$0")/com.ydisks.xianyu-helper.server.plist.template" "$APP/Contents/Resources/"
cp "$(dirname "$0")/com.ydisks.xianyu-helper.tray.plist.template" "$APP/Contents/Resources/"
sed "s/__VERSION__/$VERSION/g" "$(dirname "$0")/Info.plist" > "$APP/Contents/Info.plist"
chmod 0755 "$APP/Contents/MacOS/xianyu-tray" "$APP/Contents/Helpers/xianyu-server" "$APP/Contents/Helpers/browser-install"

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

  # macOS 代码签名必须从内部组件开始，最后再签名 App 包本身。
  sign_code "$APP/Contents/Helpers/xianyu-server"
  sign_code "$APP/Contents/Helpers/browser-install"
  sign_code "$APP/Contents/MacOS/xianyu-tray"
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
  --scripts "$(dirname "$0")/scripts" \
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
