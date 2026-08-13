#!/usr/bin/env bash
# 结构检查：文件行数 / 目录文件数 / 禁止模式 / 工具索引对账。
#
# 用法：scripts/check-structure.sh   （或 make check-structure）
# 前置：仓库根目录执行；只检查 git 跟踪 + 未忽略的新文件。
# 可安全重跑（只读）。
#
# 规则来源是根 AGENTS.md §1（阈值体系）：
#   硬线（fail）：源文件 > 800 行；目录直接源文件 > 20 个；禁止模式；索引缺条目
#   软线（warn）：Go > 400 行、TS/TSX > 300 行；目录直接源文件 > 8 个
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

FAIL=0
WARN=0

fail() { echo "✗ $1"; FAIL=$((FAIL + 1)); }
warn() { echo "⚠ $1"; WARN=$((WARN + 1)); }

# ---- 待检文件清单：git 跟踪 + 未忽略的新文件 ------------------------------
FILES=$(git ls-files --cached --others --exclude-standard -- '*.go' '*.ts' '*.tsx')

# 豁免（每条必须写理由）：
#   web/src/components/ui/       shadcn CLI 托管：文件数由 CLI 决定、不可分包，行数软线同免；行数硬线仍适用
#   web/src/i18n/locales/        语言平表天然随文案线性增长，拆分无语义；行数硬线仍适用
is_exempt_soft() {
  case "$1" in
    web/src/components/ui/*) return 0 ;;
    web/src/i18n/locales/*) return 0 ;;
  esac
  return 1
}

is_exempt_dircount() {
  case "$1" in
    web/src/components/ui) return 0 ;;
  esac
  return 1
}

# ---- 检查 1：单文件行数 ---------------------------------------------------
while IFS= read -r f; do
  [ -f "$f" ] || continue
  lines=$(wc -l <"$f" | tr -d ' ')
  if [ "$lines" -gt 800 ]; then
    fail "$f 有 $lines 行（硬线 800）——按职责拆分，方案参考 AGENTS.md §1.4"
    continue
  fi
  is_exempt_soft "$f" && continue
  case "$f" in
    *.go)
      [ "$lines" -gt 400 ] && warn "$f 有 $lines 行（Go 拆分信号 400）"
      ;;
    *.ts | *.tsx)
      [ "$lines" -gt 300 ] && warn "$f 有 $lines 行（TS 拆分信号 300）"
      ;;
  esac
done <<<"$FILES"

# ---- 检查 2：目录直接源文件数 --------------------------------------------
# _test.go 跟随其实现文件迁移，单独计数会双倍惩罚 Go 的同包测试惯例，不计。
SRC_NO_TEST=$(grep -v '_test\.go$' <<<"$FILES")
while IFS= read -r d; do
  [ -n "$d" ] || continue
  is_exempt_dircount "$d" && continue
  count=$(grep -cx "$d/[^/]*" <<<"$SRC_NO_TEST")
  if [ "$count" -gt 20 ]; then
    fail "$d/ 直接源文件 $count 个（硬线 20）——按相关性分包，见 AGENTS.md §1.3"
  elif [ "$count" -gt 8 ] && ! is_exempt_soft "$d/x"; then
    warn "$d/ 直接源文件 $count 个（审视线 8）——考虑按功能域分组"
  fi
done <<<"$(sed 's|/[^/]*$||' <<<"$SRC_NO_TEST" | sort -u)"

# ---- 检查 3：禁止模式 -----------------------------------------------------
# 3a. api.ts 之外的裸 fetch(（后端请求必须走 lib/api.ts）
hits=$(grep -rn --include='*.ts' --include='*.tsx' -E '(^|[^.a-zA-Z])fetch\(' web/src \
  | grep -v 'web/src/lib/api.ts' || true)
[ -n "$hits" ] && while IFS= read -r h; do fail "裸 fetch（必须走 lib/api.ts）：$h"; done <<<"$hits"

# 3b. 原生 confirm / alert（必须用 AlertDialog / sonner）
hits=$(grep -rn --include='*.ts' --include='*.tsx' -E 'window\.(confirm|alert)\(' web/src || true)
[ -n "$hits" ] && while IFS= read -r h; do fail "原生 confirm/alert（用 AlertDialog/sonner）：$h"; done <<<"$hits"

# 3c. 原始色板类 / 硬编码色值（视觉常量必须走 index.css 语义 token）。
#     确有理由的例外在行尾标 `// check-ignore: 理由`（如 xterm canvas 不认 CSS 变量）。
hits=$(grep -rn --include='*.tsx' -E 'text-(red|blue|green|yellow|purple|pink|orange)-[0-9]{3}|#[0-9a-fA-F]{6}|rgba?\(' web/src/components web/src/routes \
  | grep -v 'web/src/components/ui/' | grep -v 'check-ignore:' || true)
[ -n "$hits" ] && while IFS= read -r h; do fail "硬编码色值（走语义 token / index.css 深度层）：$h"; done <<<"$hits"

# 3d. 根目录游离文件（新增顶层文件先过 AGENTS.md §1.1 的蓝图）
ROOT_ALLOW="AGENTS.md CLAUDE.md Makefile README.md skills-lock.json"
while IFS= read -r f; do
  case "$f" in
    */*) continue ;;
    .*) continue ;;
  esac
  case " $ROOT_ALLOW " in
    *" $f "*) ;;
    *) fail "根目录游离文件：$f（顶层布局见 AGENTS.md §1.1，临时文件不入库）" ;;
  esac
done <<<"$(git ls-files --cached --others --exclude-standard)"

# ---- 检查 4：工具索引对账 -------------------------------------------------
# lib/ 与 hooks/ 的每个源文件必须登记在 web/src/lib/README.md
IDX=web/src/lib/README.md
if [ -f "$IDX" ]; then
  while IFS= read -r f; do
    base=$(basename "$f")
    [ "$base" = "README.md" ] && continue
    grep -q "$base" "$IDX" || fail "工具未登记索引：$f 不在 $IDX（加工具必须同步索引）"
  done <<<"$(grep -E '^web/src/(lib|hooks)/[^/]+\.(ts|tsx)$' <<<"$FILES")"
else
  fail "缺少前端工具索引 $IDX"
fi

# internal/ 的每个直接子包必须登记在 server/internal/README.md
IDX=server/internal/README.md
if [ -f "$IDX" ]; then
  while IFS= read -r pkg; do
    grep -qE "^\| $pkg " "$IDX" || fail "包未登记索引：server/internal/$pkg 不在 $IDX"
  done <<<"$(sed -nE 's|^server/internal/([^/]+)/.*|\1|p' <<<"$FILES" | sort -u)"
else
  fail "缺少后端包索引 $IDX"
fi

# ---- 汇总 -----------------------------------------------------------------
echo
if [ "$FAIL" -gt 0 ]; then
  echo "结构检查未通过：$FAIL 项违规，$WARN 项提醒"
  exit 1
fi
echo "结构检查通过（$WARN 项提醒）"
