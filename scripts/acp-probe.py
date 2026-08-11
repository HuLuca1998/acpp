#!/usr/bin/env python3
"""ACP runtime 协议探针：对指定 agent 跑 initialize / session/new，转储能力形状。

用途：
    复验 docs/adr-001 的差异清单。runtime 版本升级后跑一遍，对照
    modes / configOptions / 权限档 / 思考深度选项是否漂移——语义差异会随
    版本变化（codex 档位名 0.16 → 1.1.7 已改过一次）。

用法：
    python3 scripts/acp-probe.py codex-acp
    python3 scripts/acp-probe.py claude-agent-acp
    ACP_PROBE_PROMPT="1+1=?" python3 scripts/acp-probe.py claude-agent-acp  # 附带跑一轮（花真钱）

前置条件：
    对应 runtime 已全局安装且已登录（codex login / Claude Code 登录态）。
    脚本只建会话读能力，不发 prompt（除非设置 ACP_PROBE_PROMPT），零模型开销。
    可安全重跑；工作目录用系统临时目录，不碰任何项目文件。
"""
import json
import os
import subprocess
import sys
import tempfile
import threading
import time

if len(sys.argv) < 2:
    print(__doc__)
    sys.exit(1)

COMMAND = sys.argv[1]
CWD = os.path.join(tempfile.gettempdir(), "acpp-probe")
os.makedirs(CWD, exist_ok=True)

env = dict(os.environ)
# 嵌套会话标记不摘掉的话，从 agent 终端里启动时 runtime 会拒绝服务。
for k in ("CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT",
          "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"):
    env.pop(k, None)

proc = subprocess.Popen([COMMAND], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=subprocess.PIPE, text=True, bufsize=1, env=env)

results: dict[int, dict] = {}
notifications: list[dict] = []


def send(obj):
    proc.stdin.write(json.dumps(obj) + "\n")
    proc.stdin.flush()


def reader():
    for line in proc.stdout:
        try:
            msg = json.loads(line)
        except Exception:
            continue
        if "id" in msg and ("result" in msg or "error" in msg):
            results[msg["id"]] = msg
        elif "id" in msg and "method" in msg:
            # 反向请求给默认应答，别让 agent 卡死。
            m = msg["method"]
            if m == "session/request_permission":
                opts = msg["params"].get("options", [])
                opt = next((o for o in opts if o.get("kind") == "allow_once"),
                           opts[0] if opts else None)
                send({"jsonrpc": "2.0", "id": msg["id"], "result":
                      {"outcome": {"outcome": "selected", "optionId": opt["optionId"]}}
                      if opt else {"outcome": {"outcome": "cancelled"}}})
            else:
                send({"jsonrpc": "2.0", "id": msg["id"], "result": {}})
        elif msg.get("method"):
            notifications.append(msg)


def errreader():
    for line in proc.stderr:
        print("STDERR:", line.rstrip()[:200], file=sys.stderr)


threading.Thread(target=reader, daemon=True).start()
threading.Thread(target=errreader, daemon=True).start()


def wait(i, n=300):
    for _ in range(n):
        if i in results:
            return results[i]
        time.sleep(0.2)
    print(f"timeout waiting for response {i}", file=sys.stderr)
    proc.kill()
    sys.exit(2)


send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
    "protocolVersion": 1,
    "clientCapabilities": {
        "fs": {"readTextFile": True, "writeTextFile": True},
        "terminal": False,
        "elicitation": {"form": {}},
    }}})
print("== initialize ==")
print(json.dumps(wait(1), ensure_ascii=False, indent=1))

send({"jsonrpc": "2.0", "id": 2, "method": "session/new",
      "params": {"cwd": CWD, "mcpServers": []}})
r = wait(2)
print("== session/new ==")
print(json.dumps(r, ensure_ascii=False, indent=1))

prompt = os.environ.get("ACP_PROBE_PROMPT")
if prompt and "result" in r:
    sid = r["result"]["sessionId"]
    send({"jsonrpc": "2.0", "id": 3, "method": "session/prompt", "params": {
        "sessionId": sid, "prompt": [{"type": "text", "text": prompt}]}})
    print("== session/prompt ==")
    print(json.dumps(wait(3, 900), ensure_ascii=False, indent=1))
    print("== notifications ==")
    for n in notifications:
        print(json.dumps(n, ensure_ascii=False)[:300])

proc.kill()
