#!/usr/bin/env bash
# 用途：起一份「预览壳」看桌面版界面/窗口的改动效果，**不碰已安装的 ACPP.app**。
#       壳走 ACPP_DEV_URL 分支：只开窗口加载开发前端，不启动 acp-server、
#       不占 48090、不放菜单栏图标（见 desktop/electron/main/index.js）。
#       与正式版的隔离靠壳自己改名换 userData（打包产物里的 package.json name
#       和开发态相同，不改就会被正在运行的 ACPP.app 当成第二个实例挤掉），
#       两份因此可以同时开着对比。
# 用法：scripts/dev-app.sh
#       ACPP_DEV_URL   覆盖要加载的地址，默认 http://localhost:45173/（vite）
# 前置：make install（装过 desktop/electron 的 electron 依赖）。
# 可安全重跑：开发服务按 dev.sh 的老规矩清端口重启，48090 全程不动。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

URL="${ACPP_DEV_URL:-http://localhost:45173/}"

# 只在用默认地址（本机 vite）时才代管开发服务；指到别处说明用户自有主张。
if [ "$URL" = "http://localhost:45173/" ]; then
  scripts/dev.sh start all
fi

[ -d desktop/electron/node_modules/electron ] || {
  echo "缺 electron 依赖，先跑 make install" >&2
  exit 1
}

echo "==> 预览壳加载 $URL（关窗即退出；开发菜单里有刷新与开发者工具）"
cd desktop/electron
ACPP_DEV_URL="$URL" exec npm start
