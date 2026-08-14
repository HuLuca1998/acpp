#!/usr/bin/env bash
# 用途：打包 macOS 桌面版——build/app/ACPP.app（Swift 菜单栏壳 + 捆绑
#       acp-server + 前端产物 + 程序化生成的图标，ad-hoc 签名）。
# 用法：scripts/build-macos-app.sh [--skip-web]
#       --skip-web    跳过前端构建，复用已有 build/web（迭代壳代码时提速）
#       APP_VERSION   环境变量覆盖版本号，默认 0.1.0
# 前置：macOS + Xcode Command Line Tools（swiftc/iconutil/codesign）、node、go。
# 可安全重跑（每次全量重组 bundle）。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

APP_NAME="ACPP"
VERSION="${APP_VERSION:-0.1.0}"
OUT="$ROOT/build/app"
BUNDLE="$OUT/$APP_NAME.app"
STAGE="$OUT/stage"

SKIP_WEB=0
[ "${1:-}" = "--skip-web" ] && SKIP_WEB=1

mkdir -p "$STAGE"

echo "==> 前端产物"
if [ "$SKIP_WEB" = 1 ] && [ -f "$ROOT/build/web/index.html" ]; then
  echo "    复用 build/web"
else
  (cd web && npm run build)
fi
[ -f "$ROOT/build/web/index.html" ] || { echo "缺 build/web/index.html，前端构建失败？" >&2; exit 1; }

echo "==> 编译 acp-server"
# 版本与发布仓库经 ldflags 注入 config 包：健康接口与更新检查都读它们。
UPDATE_REPO="${ACP_UPDATE_REPO:-}"
if [ -z "$UPDATE_REPO" ]; then
  ORIGIN="$(git -C "$ROOT" remote get-url origin 2>/dev/null || true)"
  # 两步剥离（BSD sed 的 ERE 不支持非贪婪）：去 host 前缀，再去 .git 后缀
  UPDATE_REPO="$(printf '%s' "$ORIGIN" | sed -nE 's#.*github\.com[:/]##p' | sed 's#\.git$##')"
fi
LDFLAGS="-s -w -X acpp/server/internal/config.Version=$VERSION"
if [ -n "$UPDATE_REPO" ]; then
  LDFLAGS="$LDFLAGS -X acpp/server/internal/config.DefaultUpdateRepo=$UPDATE_REPO"
fi
(cd server && go build -trimpath -ldflags "$LDFLAGS" -o "$STAGE/acp-server" ./cmd/server)

echo "==> 生成图标"
rm -rf "$STAGE/icons"
mkdir -p "$STAGE/icons"
swift desktop/macos/IconGen/icongen.swift "$STAGE/icons" >/dev/null
iconutil -c icns "$STAGE/icons/AppIcon.iconset" -o "$STAGE/AppIcon.icns"

echo "==> 编译壳应用"
swiftc -O desktop/macos/Sources/*.swift -o "$STAGE/shell"

echo "==> 组装 bundle"
rm -rf "$BUNDLE"
mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources"
cp "$STAGE/shell" "$BUNDLE/Contents/MacOS/$APP_NAME"
cp "$STAGE/acp-server" "$BUNDLE/Contents/MacOS/acp-server"
cp -R "$ROOT/build/web" "$BUNDLE/Contents/Resources/web"
cp "$STAGE/AppIcon.icns" "$BUNDLE/Contents/Resources/AppIcon.icns"
cp "$STAGE/icons/MenuBarIcon.png" "$STAGE/icons/MenuBarIcon@2x.png" "$BUNDLE/Contents/Resources/"
sed "s/@VERSION@/$VERSION/g" desktop/macos/Info.plist.in >"$BUNDLE/Contents/Info.plist"
printf 'APPL????' >"$BUNDLE/Contents/PkgInfo"

echo "==> 签名（ad-hoc，本机分发够用；过公证需要开发者证书另说）"
codesign --force --sign - "$BUNDLE/Contents/MacOS/acp-server"
codesign --force --sign - "$BUNDLE"

echo
echo "完成：$BUNDLE（$(du -sh "$BUNDLE" | cut -f1 | tr -d ' ')）"
echo "运行：open \"$BUNDLE\""
