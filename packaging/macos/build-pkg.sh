#!/bin/sh
set -eu

VERSION="${1:?version is required}"
DIST_DIR="${2:?dist directory is required}"
ROOT_DIR="$DIST_DIR/pkgroot"
APP="$ROOT_DIR/Applications/Ydisks Xianyu Helper.app"

rm -rf "$ROOT_DIR"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Helpers" "$APP/Contents/Resources"
cp "$DIST_DIR/xianyu-server" "$APP/Contents/Helpers/xianyu-server"
cp "$DIST_DIR/browser-install" "$APP/Contents/Helpers/browser-install"
cp "$DIST_DIR/xianyu-tray" "$APP/Contents/MacOS/xianyu-tray"
cp "$(dirname "$0")/com.christ.ydisks-xianyu-helper.server.plist.template" "$APP/Contents/Resources/"
cp "$(dirname "$0")/com.christ.ydisks-xianyu-helper.tray.plist.template" "$APP/Contents/Resources/"
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

pkgbuild \
  --root "$ROOT_DIR" \
  --scripts "$(dirname "$0")/scripts" \
  --identifier com.christ.ydisks-xianyu-helper \
  --version "$VERSION" \
  --install-location / \
  "$DIST_DIR/Ydisks-Xianyu-Helper-$VERSION.pkg"
