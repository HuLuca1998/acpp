#!/usr/bin/env bash
# 用途：打包并发布 macOS 桌面版到 GitHub Releases——构建 ACPP.app → zip →
#       git tag → gh release create（附 release notes，App 内更新检查读它）。
# 用法：scripts/release-macos.sh <版本号> [notes文件]
#       版本号形如 0.2.0（不带 v）；notes 缺省用上个 tag 以来的 git log 生成。
# 前置：gh CLI 已登录、仓库有 GitHub remote、工作区干净（未提交改动拒绝发布）。
# 重跑须知：同一版本号重复执行会因 tag 已存在而失败——这是防重复发布的保护。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${1:?用法: scripts/release-macos.sh <版本号> [notes文件]}"
NOTES_FILE="${2:-}"
TAG="v$VERSION"

[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "版本号必须形如 0.2.0（不带 v）" >&2; exit 1; }
git diff --quiet && git diff --cached --quiet || { echo "工作区有未提交改动，先提交再发布" >&2; exit 1; }
git rev-parse -q --verify "refs/tags/$TAG" >/dev/null && { echo "tag $TAG 已存在，换个版本号" >&2; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "gh 未登录：先跑 gh auth login" >&2; exit 1; }

# release notes：显式给文件用文件，否则汇总上个 tag 以来的提交标题
if [ -z "$NOTES_FILE" ]; then
  NOTES_FILE="$(mktemp)"
  LAST_TAG="$(git describe --tags --abbrev=0 2>/dev/null || true)"
  RANGE="${LAST_TAG:+$LAST_TAG..HEAD}"
  git log ${RANGE:+"$RANGE"} --no-merges --pretty='- %s' >"$NOTES_FILE"
  echo "==> release notes（自动生成，来自 git log）"
  sed 's/^/    /' "$NOTES_FILE"
fi

echo "==> 构建 ACPP.app（版本 $VERSION）"
# 发布构建落在独立目录：build/app 里可能就是本机正在运行的 ACPP.app，
# 发布不该动用户的日常使用。
RELEASE_OUT="$ROOT/build/release"
APP_VERSION="$VERSION" APP_OUT="$RELEASE_OUT" scripts/build-macos-app.sh

echo "==> 打 zip"
ZIP="$RELEASE_OUT/ACPP-$VERSION.zip"
rm -f "$ZIP"
ditto -ck --keepParent "$RELEASE_OUT/ACPP.app" "$ZIP"

echo "==> 打 tag 并推送"
git tag -a "$TAG" -m "ACPP $VERSION"
git push origin HEAD "$TAG"

echo "==> 创建 GitHub Release"
gh release create "$TAG" "$ZIP" --title "ACPP $VERSION" --notes-file "$NOTES_FILE"

echo
echo "发布完成：$TAG（asset: $(basename "$ZIP")）"
echo "App 内「设置 → 关于与更新」将在下次检查时看到这个版本。"
