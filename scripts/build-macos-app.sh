#!/usr/bin/env bash
# 用途：打包 macOS 桌面版——build/app/ACPP.app（Electron 壳 + 捆绑 acp-server
#       + 前端产物 + 程序化生成的图标，ad-hoc 签名）。
# 用法：scripts/build-macos-app.sh [--skip-web]
#       --skip-web    跳过前端构建，复用已有 build/web（迭代壳代码时提速）
#       APP_VERSION   环境变量覆盖版本号，默认 0.1.0
#       APP_OUT       环境变量覆盖输出目录，默认 build/app——发布流程用
#                     独立目录，避免覆盖本机正在运行的 ACPP.app
# 前置：macOS + Xcode Command Line Tools（swift/iconutil/codesign）、node、go。
# 可安全重跑（每次全量重组 bundle）。
#
# 壳为什么是 Electron 而不是 Swift/WKWebView：见 docs/adr-015。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

APP_NAME="ACPP"
BUNDLE_ID="app.acpp.console"
VERSION="${APP_VERSION:-0.1.0}"
OUT="${APP_OUT:-$ROOT/build/app}"
BUNDLE="$OUT/$APP_NAME.app"
STAGE="$OUT/stage"
SHELL_DIR="$ROOT/desktop/electron"

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
swift desktop/icons/icongen.swift "$STAGE/icons" >/dev/null
iconutil -c icns "$STAGE/icons/AppIcon.iconset" -o "$STAGE/AppIcon.icns"

echo "==> 准备壳依赖"
# electron 与 packager 都在 devDependencies；已装过就跳过，保持可重跑。
if [ ! -d "$SHELL_DIR/node_modules/electron" ] || [ ! -d "$SHELL_DIR/node_modules/@electron/packager" ]; then
  (cd "$SHELL_DIR" && npm install --no-audit --no-fund)
fi

echo "==> 打包 Electron 壳"
# 架构跟随本机：uname 的 arm64/x86_64 要翻成 packager 认的 arm64/x64。
case "$(uname -m)" in
  arm64) ARCH="arm64" ;;
  x86_64) ARCH="x64" ;;
  *) echo "不支持的架构：$(uname -m)" >&2; exit 1 ;;
esac
PACKED="$STAGE/packed"
rm -rf "$PACKED"
(cd "$SHELL_DIR" && npx --no-install @electron/packager . "$APP_NAME" \
  --platform=darwin --arch="$ARCH" \
  --app-bundle-id="$BUNDLE_ID" \
  --app-version="$VERSION" --build-version="$VERSION" \
  --app-category-type=public.app-category.developer-tools \
  --icon="$STAGE/AppIcon.icns" \
  --extend-info="$SHELL_DIR/Info.extend.plist" \
  --out="$PACKED" --overwrite --quiet)

rm -rf "$BUNDLE"
mkdir -p "$OUT"
mv "$PACKED/$APP_NAME-darwin-$ARCH/$APP_NAME.app" "$BUNDLE"

echo "==> 塞入后端与前端产物"
# acp-server 的位置是契约：后端自更新按 bundle 结构找壳进程（server/internal/
# system/update.go 的 shellProcessID），换位置要连着改那边。
cp "$STAGE/acp-server" "$BUNDLE/Contents/MacOS/acp-server"
rm -rf "$BUNDLE/Contents/Resources/web"
cp -R "$ROOT/build/web" "$BUNDLE/Contents/Resources/web"
cp "$STAGE/icons/MenuBarIcon.png" "$STAGE/icons/MenuBarIcon@2x.png" "$BUNDLE/Contents/Resources/"

echo "==> 签名（ad-hoc，本机分发够用；过公证需要开发者证书另说）"
# 先签内部可执行文件再签整体：Electron 的 bundle 里还有 framework 与 helper
# app，--deep 一次性覆盖它们（ad-hoc 够用，正式公证要逐个签）。
codesign --force --sign - "$BUNDLE/Contents/MacOS/acp-server"
codesign --force --deep --sign - "$BUNDLE"

# 中间产物没有留存价值——图标、acp-server、packager 的输出每次都全量重生成，
# 而 packager 那份是 .app 的完整副本，不清的话每打一次包就白占几十 M。
# 只在成功后清：中途失败时现场更值钱（set -e 会在失败处直接退出，走不到这里）。
echo "==> 清理中间产物"
rm -rf "$STAGE"

echo
echo "完成：$BUNDLE（$(du -sh "$BUNDLE" | cut -f1 | tr -d ' ')）"
echo "运行：open \"$BUNDLE\""
