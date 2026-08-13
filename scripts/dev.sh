#!/usr/bin/env bash
# acpp 开发服务管理：启动 / 停止 / 重启 / 状态，唯一入口。
#
# 用法：
#   scripts/dev.sh start   [server|web|all]   # 起服务（先清掉端口上的旧进程）
#   scripts/dev.sh restart [server|web|all]   # 同 start；后端会先重新编译（即“更新”）
#   scripts/dev.sh stop    [server|web|all]
#   scripts/dev.sh status
#
# 端口是项目固定约定（根 AGENTS.md §4.1）：后端 48080、前端 45173。
# 端口被占一律杀掉旧进程重用，绝不另起新端口。
# 可安全重跑；日志在 ${TMPDIR:-/tmp}/acpp-dev/ 下。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SERVER_PORT=48080
WEB_PORT=45173
LOG_DIR="${TMPDIR:-/tmp}/acpp-dev"
mkdir -p "$LOG_DIR"

kill_port() {
  local port=$1
  local pids
  pids=$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
  if [ -n "$pids" ]; then
    echo "端口 $port 被占（pid $pids），清掉旧进程"
    kill $pids 2>/dev/null || true
    sleep 1
    pids=$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
    if [ -n "$pids" ]; then
      kill -9 $pids 2>/dev/null || true
    fi
  fi
}

wait_http() {
  local url=$1 name=$2
  for _ in $(seq 1 40); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      echo "$name 已就绪：$url"
      return 0
    fi
    sleep 0.5
  done
  echo "$name 没有按时起来，看日志：$LOG_DIR" >&2
  return 1
}

start_server() {
  kill_port "$SERVER_PORT"
  # 每次启动前重新编译：restart 即更新，跑的永远是当前代码。产物统一进顶层 build/。
  echo "编译后端…"
  (cd "$ROOT/server" && go build -o ../build/server/acp-server ./cmd/server)
  echo "启动后端 :$SERVER_PORT（日志 $LOG_DIR/server.log）"
  # </dev/null 切断与调用方 stdio 的关联，否则 make dev | tail 这类管道会被挂住。
  (cd "$ROOT/server" && ACP_DEBUG=1 nohup ../build/server/acp-server >"$LOG_DIR/server.log" 2>&1 </dev/null &)
  wait_http "http://127.0.0.1:$SERVER_PORT/api/health" "后端"
}

start_web() {
  kill_port "$WEB_PORT"
  echo "启动前端 :$WEB_PORT（日志 $LOG_DIR/web.log）"
  (cd "$ROOT" && nohup npm run dev --prefix web >"$LOG_DIR/web.log" 2>&1 </dev/null &)
  wait_http "http://localhost:$WEB_PORT" "前端"
}

status() {
  local pair name port pids
  for pair in "后端:$SERVER_PORT" "前端:$WEB_PORT"; do
    name=${pair%%:*}
    port=${pair##*:}
    pids=$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
    if [ -n "$pids" ]; then
      echo "$name：运行中（端口 $port，pid $pids）"
    else
      echo "$name：未运行（端口 $port）"
    fi
  done
}

ACTION=${1:-status}
TARGET=${2:-all}

case "$ACTION" in
  start | restart)
    if [ "$TARGET" = server ] || [ "$TARGET" = all ]; then start_server; fi
    if [ "$TARGET" = web ] || [ "$TARGET" = all ]; then start_web; fi
    ;;
  stop)
    if [ "$TARGET" = server ] || [ "$TARGET" = all ]; then kill_port "$SERVER_PORT"; fi
    if [ "$TARGET" = web ] || [ "$TARGET" = all ]; then kill_port "$WEB_PORT"; fi
    echo "已停止：$TARGET"
    ;;
  status)
    status
    ;;
  *)
    echo "用法：$0 {start|stop|restart|status} [server|web|all]" >&2
    exit 1
    ;;
esac
