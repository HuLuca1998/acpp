---
name: discord-e2e
description: 在真实 Discord 上端到端测 acpp 频道工作区的操作手册。要验证 /init 绑定、子区对话、数据库锁定、/git、解绑清理这类只有真机才能确认的行为时使用；覆盖建/删测试频道、驱动斜杠命令与组件、@bot 发消息、从后端侧取证、收尾清理。关键词：discord 真机测试、测 /init、测频道绑定、discord 自动化、浏览器操作 discord、斜杠命令发不出去、下拉点不开。
---

# Discord 真机测试手册

acpp 的 discord 子系统有大量行为只有真机能验证：Discord 的表单限制、组件交互、
mention 解析、频道事件。这份手册是**怎么跑一轮完整测试**——功能设计见
[docs/adr-016](../../../docs/adr-016-discord-频道工作区.md) /
[adr-017](../../../docs/adr-017-discord-子区对话.md) /
[adr-018](../../../docs/adr-018-discord-工作树与数据库绑定.md)，包规范见
[server/internal/discord/AGENTS.md](../../../server/internal/discord/AGENTS.md)。

**三条铁律**：

1. **只在 `Acpp-Test` 服务器里测**（guild `1539549447283806288`）。别的服务器一概
   不碰，建频道时 guild id 写死这一个。
2. **只碰自己这轮建的测试频道**。`#pp-game`、`#test` 这类已有频道是用户在用的
   ——不 `/init`、不 `/unbind`、不删、也不动它们的工作树目录。踩过：清理测试
   残留时把用户在另一个会话里绑的频道目录一起删了。
3. 测试只在 dev 后端（48080）上做，不碰用户正在用的 app（48090）；测试频道、
   工作树、分支结束时收干净。

## 1. 开场：三件事就位

```bash
# ① dev 后端起着且是当前代码（restart 会先重新编译）
scripts/dev.sh restart server            # 后台跑，别前台等
until [ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:48080/api/health)" = 200 ]; do sleep 3; done

# ② bot 连上了（日志出现这行才算）
grep -i "已上线" "${TMPDIR:-/tmp}/acpp-dev/server.log" | tail -1

# ③ bot 在哪个服务器、现有绑定是什么
curl -s http://127.0.0.1:48080/api/discord | python3 -m json.tool | head -40
```

bot token 在 `~/.acpp-dev/discord.json`（dev）。**取出来用变量接，别打印到输出里**：

```bash
TOKEN=$(python3 -c "import json;print(json.load(open('$HOME/.acpp-dev/discord.json'))['botToken'])")
```

**改了命令表（`slashCommands`）必须重启后端**——命令是连上 gateway 时注册的；
而且 **Discord 客户端缓存命令列表，网页要刷新一次**才认得新命令，否则输入
`/xxx` 不弹浮层，很容易误判成"命令没注册"。

## 2. 登录：只能用户自己来

打开网页版让用户登录，**绝不代填密码**：

```
mcp__Claude_Browser__preview_start  { url: "https://discord.com/app" }
```

截图确认停在账号选择/登录页 → 让用户点登录并输密码 → 等用户回话再继续。

## 3. 频道管理：走 bot API，别用 UI

建频道、删频道用 REST 比点 UI 快一个数量级，也不会误触：

**guild 固定是 `1539549447283806288`（Acpp-Test）**，别从别处取。频道名带个能
一眼认出的前缀（如 `pp-`），收尾时才好确认哪些是自己建的。

```bash
GUILD=1539549447283806288                    # Acpp-Test，写死
# 建
curl -s -X POST "https://discord.com/api/v10/guilds/$GUILD/channels" \
  -H "Authorization: Bot $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"pp-prod","type":0}' | python3 -c "import json,sys;d=json.load(sys.stdin);print(d.get('name'),d.get('id'))"

# 删（会触发后端的 CHANNEL_DELETE 自动解绑，这本身就是一条要测的路径）
# 只删这轮自己建的那几个 id——删之前把 id 与建频道时记下的对上，别照名字猜
curl -s -X DELETE "https://discord.com/api/v10/channels/<自己建的 channelId>" -H "Authorization: Bot $TOKEN"
```

切频道用 URL 直达，比点侧边栏可靠（侧边栏 ref 会随渲染失效）：

```
mcp__Claude_Browser__navigate { url: "https://discord.com/channels/<guildId>/<channelId>" }
```

## 4. 驱动 UI：三个必须知道的坑

### 4.1 只有 `type` 和 `click` 有效，`key` 事件全不生效

内嵌浏览器里 `computer key`（`Enter`/`Return`/`Backspace`/`cmd+a` 都试过）
**送不进 Discord 输入框**。所以：

- **发送**：JS 往编辑器 dispatch keydown Enter
  ```js
  const el = document.querySelector('[data-slate-editor][aria-label*="发消息"]');
  el.focus();
  el.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', code:'Enter', keyCode:13, which:13, bubbles:true, cancelable:true}));
  ```
- **清空输入框**：清不掉。`execCommand('delete')` 只改 DOM，Slate 内部 state
  还留着旧内容，发出去的是旧的（真机踩过：想发长消息，实际只发出一个残留的 `a`）。
  唯一可靠的办法是**把它发出去**，或者换一个干净频道操作。刷新页面会恢复草稿，没用。
- **子区里发消息：先把子区当独立频道打开**。子区开在右侧面板时，同一段 JS 派发
  Enter **不生效**（消息留在输入框里，试了 keydown/keypress/keyup 全套、focus 过、
  选择器也确认选中的是子区那个 editor）。导航到 `/channels/<guildId>/<threadId>`
  把子区变成主视图就正常了——顺带草稿也是干净的（草稿绑在面板上）。
  子区 id 从后端拿：`~/.acpp-dev/discord.json` 的 `threads[].threadId`。

### 4.1.5 面板是和用户共用的：每一步动手前核对 tab 标题里的频道名

内嵌浏览器面板用户随时可能切去别的服务器看自己的东西。真机踩过：上一步还在
`#ppgame-live`，下一步 `type "/cron"` 时面板已经在用户的正式频道里，命令直接
打进了那个输入框（幸好没发）。所以 **每个 batch 的第一步先 `screenshot` 或看
上一次结果里的 `Tab Context` 标题**，确认还是 `#<测试频道> | Acpp-Test` 再
type / click；发现不对立刻停手告诉用户，别自己去清人家输入框里的草稿。

### 4.2 斜杠命令要「点浮层选中」再发

```
computer left_click  → 输入框
computer type        → "/init"
（等 2-3 秒浮层出现）
computer left_click  → 浮层里那一条命令
（JS dispatch Enter 发送）
```

浮层不出现的三种原因：命令没注册（重启后端）、客户端缓存旧命令表（刷新页面）、
输入框里有残留使 `/` 不在行首。

带参数的命令（如 `/db source:…`）：选中命令后会列出参数，点参数名 → 弹选项列表 →
点选项，再发送。

带 choices 的参数（如 `/cron action:…`）实测顺序，用 `find` 拿 ref 比坐标稳
（面板尺寸会变，坐标一变就点空）：

```
type "/cron" → find "定时任务：看清单"（浮层那条）→ click ref
→ find "action" → click ref（参数名）→ find "remove" → click ref（选项）
→ find "任务 id 或名字前缀"（id 参数那行）→ click ref → type 值
→ JS dispatch Enter（4.1）
```

ephemeral 回复只有本人可见、bot API 拉不到；结论看截图或后端（5.）。

### 4.3 下拉/选项要 JS 派发完整指针序列

`computer click` 点不开 Discord 的 select（分支下拉、数据库下拉都是）。用：

```js
const fire = el => { const r = el.getBoundingClientRect();
  const o = {bubbles:true, cancelable:true, clientX:r.x+r.width/2, clientY:r.y+r.height/2, button:0, pointerId:1, isPrimary:true};
  el.dispatchEvent(new PointerEvent('pointerdown', o)); el.dispatchEvent(new MouseEvent('mousedown', o));
  el.dispatchEvent(new PointerEvent('pointerup', o));   el.dispatchEvent(new MouseEvent('mouseup', o));
  el.dispatchEvent(new MouseEvent('click', o)); };
// 展开：找 placeholder 文本所在元素的 [role="button"] 祖先
// 选项：[role="option"] 里按文本找，再 fire 一次
```

modal 里的 radio 是**原生 input**，可以直接 `.click()`：

```js
document.querySelector('input[type=radio][value="claude|default"]').click();
```

**时序**：modal 要等它真的渲染出来再填，提前 type 会打进频道输入框（踩过）。
先截图确认 modal 在，再填。

### 4.3.5 JS 一律指定 tabId，输入文本用 beforeinput

**`javascript_tool` 默认打在「当前活动标签」上**，而开过第二个标签（比如
同时看着 acpp 前端）之后，活动标签可能已经不是 Discord 了——脚本会静默地
跑在错的页面上，返回 `document.querySelector(...) 是 undefined` 或者干脆
一切正常但消息没发出去。**每次调用都显式传 `tabId`**。

往编辑器打字，`computer type` 有时进不去 Slate 的内部 state（发出去是空的）。
更可靠的是直接派发 `beforeinput`：

```js
el.focus();
el.dispatchEvent(new InputEvent('beforeinput',
  {inputType:'insertText', data:'要发的内容', bubbles:true, cancelable:true}));
// 然后照 4.1 派发 Enter
```

### 4.4 @bot 用原始 mention 语法

提及浮层在自动化输入下常常不弹。直接打 `<@1542460151817044071>`，Discord 客户端
会自动渲染成真 mention：

```
computer type → "<@1542460151817044071> 用 db_sources 列出你能访问的数据源"
（JS dispatch Enter）
```

## 5. 取证：结论从后端拿，不从截图猜

截图只能证明"界面长这样"，功能对不对要从后端看：

```bash
# 绑定落盘（分支/base/锁定的库/工作目录）
python3 -c "
import json
d=json.load(open('$HOME/.acpp-dev/discord.json'))
for b in d.get('bindings',[]): print(b['channelName'],'|',b.get('branch'),'|',b.get('base'),'|',b.get('dataSourceRef'),'|',b['workdir'])
"

# AI 到底看见了哪些数据源（MCP 工具调用的真实返回）
sqlite3 -line "$HOME/.acpp-dev/acp.db" \
  "select cwd, result from mcp_calls where tool='db_sources' order by id desc limit 2;"

# 工作树与分支
git -C ~/acpp/discord/<组织>/<仓库>/.repo worktree list
git -C ~/acpp/discord/<组织>/<仓库>/.repo branch --list 'discord/*'

# 后端日志（自动解绑、清理结果、限速重试都在这）
grep -iE "自动解绑|工作树|主题|已上线" "${TMPDIR:-/tmp}/acpp-dev/server.log" | tail
```

等异步结果**用条件轮询，不要 sleep 固定秒数**：

```bash
until [ -d ~/acpp/discord/<组织>/<仓库>/.worktree/<分支目录> ]; do sleep 4; done
until [ "$(sqlite3 "$HOME/.acpp-dev/acp.db" "select count(*) from mcp_calls where tool='db_sources'")" -ge 2 ]; do sleep 5; done
```

## 6. 一轮完整回归都测什么

| # | 动作 | 从哪确认 |
| --- | --- | --- |
| 1 | `/init`：仓库 → base 分支 → 数据库 → 服务器 | 绑定落盘的 branch/base/dataSourceRef/serverName；频道主题里有「服务器：」那一段；`.worktree/<分支>` 出现；`.repo` 只有一份 |
| 2 | 第二、三个频道各绑不同 base + 不同库 | 三条分支名/目录互不相同；`.repo` 体积几乎不涨（objects 共享） |
| 3 | 频道主题与置顶手册 | 截图：主题是绑定信息、手册是用法，两者都带当前分支与锁定的库 |
| 4 | `/status` `/mcps` | 显示锁定的库 |
| 5 | `/git`（干净树 / 造改动后） | 改+增+删三类分类正确；显示与 base 的差距 |
| 6 | 子区问 AI「用 db_sources 列出数据源」 | `mcp_calls` 里只返回锁定的那一条；换个频道再问，返回的是它自己那条 |
| 6b | 子区问「你能看到哪些服务器」（adr-019） | `mcp_calls` 的 `server_hosts` 只返回频道锁定的那一台，全局有几台不影响 |
| 6c | 子区里发「<@同事> 看下报告」（不 @ bot） | bot 不回、消息不打 ⏳；后端日志无新回合。再发「<@同事> <@bot> 一起看」应照常入队 |
| 7 | `/db source:…` 换绑 | 绑定更新 + 日志有会话 exited；再问一次 AI，返回的是新库 |
| 8 | 工作分支能提交 | 在工作树里 commit 成功，`origin/<base>` 不动 |
| 9 | `/unbind`（干净树） | 目录、worktree 注册、分支、绑定记录全清 |
| 10 | 删频道（树里有未提交改动） | 日志「频道已删除，自动解绑」+ 工作树**保留**，文件还在 |

造改动/回滚的手法（测 6、8、10 用）：

```bash
W=~/acpp/discord/<组织>/<仓库>/.worktree/<分支目录>
echo "// probe" >> $W/AGENTS.md; echo probe > $W/new.txt; rm -f $W/Dockerfile   # 改+增+删
git -C $W checkout -- . && rm -f $W/new.txt                                     # 还原
git -C $W reset --hard --quiet origin/<base>                                    # 连提交一起退
```

## 7. 收尾：必须清干净

```bash
# ① 解绑。注意：这个 API **只删绑定记录，不动工作树**（斜杠命令 /unbind 才
#    走完整清理）——所以第 ④ 步要自己收工作树与分支，只收自己建的那棵。
curl -s -X DELETE "http://127.0.0.1:48080/api/discord/bindings/<channelId>"
# ② 删测试频道
curl -s -X DELETE "https://discord.com/api/v10/channels/<channelId>" -H "Authorization: Bot $TOKEN"
# ③ 确认没有孤儿子区记录
python3 -c "
import json
d=json.load(open('$HOME/.acpp-dev/discord.json'))
bound={b['channelId'] for b in d.get('bindings',[])}
print([t['threadId'] for t in d.get('threads',[]) if t['channelId'] not in bound] or '无孤儿')
"
# ④ 收掉自己建的工作树与分支（解绑 API 不做这件事）。
#    **只动自己这轮建的那棵**，按 channelId 反查 workdir，别按仓库名一锅端。
R=~/acpp/discord/<组织>/<仓库>/.repo
git -C "$R" worktree remove ~/acpp/discord/<组织>/<仓库>/.worktree/<自己的分支目录>
git -C "$R" branch -D discord/<自己的频道名>
git -C "$R" worktree list        # 核对：别人的树必须原样都在
```

测试过程中造的改动、探针提交、临时文件，**在报告结论前还原**——用户的工作树不是
草稿纸。

**删任何目录前先对账**：`discord.json` 里除了自己建的频道，还有用户在用的绑定
（`#pp-game` 这类），它们的工作树不能碰。真机踩过一次——清理测试残留时按仓库名
一锅端，把用户在另一个会话里绑的频道目录也删了。正确做法是按**自己建的那几个
channelId** 反查 workdir，只删这些。

## 8. 排查表

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 「该应用程序未响应」 | 后端 400/超时，多半是组件超限 | 看日志的 `弹 /init 表单失败`；modal **最多 5 个组件** |
| 输入 `/xxx` 不弹浮层 | 命令没注册 / 客户端缓存 / 输入框有残留 | 重启后端 → 刷新页面 → 换干净频道 |
| 发出去的是旧内容 | Slate state 与 DOM 不同步 | 把草稿发掉，或换频道 |
| 下拉点不开 | Discord select 不吃合成 click | JS 派发 pointerdown/mousedown/pointerup/mouseup/click |
| 改主题没生效 | 平台限速（每频道 10 分钟 2 次，429 的 retry_after 能到 5 分钟） | 后端已挂延迟重写，等一会看日志 |
| `git worktree remove` 失败 | 工作目录被 acp 进程占着 | 先关子区会话（解绑流程里已经先关了） |
