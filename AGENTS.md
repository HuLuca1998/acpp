# ACPP 工程规范（根）

本文件是本项目所有开发者——包括 Claude、Codex 等 AI 协作者——共同遵守的**通用规范**。
项目背景、架构、API、数据流见 [README.md](README.md)，动手前先读它。

规范按目录分层维护，**在哪个包干活就必须读哪份**：

- 本文件 — 跨前后端的通用规则（分包、API 契约、提交、验证）
- [server/AGENTS.md](server/AGENTS.md) — 后端规范（Go 分层、错误处理、命名、测试）
- [web/AGENTS.md](web/AGENTS.md) — 前端规范 + 设计规范（目录职责、i18n、UI/UX）

规范冲突时的优先级：子目录规范 > 本文件 > 现有代码惯例 > 个人偏好。发现规范与代码不一致，以规范为准并顺手修正代码；认为规范本身不合理，先改规范（并说明理由），再改代码。

## 0. 总则

- **最小改动**：只改与任务相关的代码，不顺手重构、不顺手改格式。重构单独开任务。
- **改完必验证**：见 [§4 验证与提交](#4-验证与提交)。没跑验证不算完成。
- **文档随代码走**：改了 API、目录结构、配置项、SSE 事件，同一次提交里更新 README 对应小节。
- **不确定就看邻居**：写新代码前，先找同层的现有文件照着写。

## 1. 目录与分包规范

### 1.1 顶层布局

```
acpp/
├── AGENTS.md          # 通用规范（本文件，CLAUDE.md 指向它）
├── README.md          # 架构与使用文档
├── Makefile           # 所有常用命令的唯一入口，make help 查看
├── skills-lock.json   # skills CLI 锁文件，与 .claude/skills/ 同步提交
├── .claude/skills/    # 第三方 skill 知识库，CLI 管理，禁止手改（§3.3）
├── docs/              # 决策记录（ADR）与专题文档（§3.1）
├── scripts/           # 开发辅助脚本：dev.sh、check-structure.sh 等（§3.2）
├── build/             # 编译产物（build/web + build/server + build/app），不入库
├── desktop/           # macOS 桌面壳（Swift/AppKit 菜单栏应用，选型见 docs/adr-004），打包走 scripts/build-macos-app.sh
├── web/               # 前端（Vite + React 19 + TS），规范见 web/AGENTS.md
└── server/            # 后端（Go），规范见 server/AGENTS.md
```

除上表之外新增顶层目录需要充分理由（**不预建空目录**，首次需要时落位并更新本表）；根目录不允许出现游离文件（`check-structure` 强制，白名单在脚本里）。

### 1.2 分包原则：按用途分，不按类型堆

包/目录的边界是「职责」，不是「文件类型」。判断标准：**能用一句话说清这个目录负责什么，且不需要用"和"连接两件事**。

- ✅ `server/internal/acp/` — 负责 ACP 协议客户端（协议类型、连接、会话池）
- ✅ `web/src/i18n/` — 负责多语言（初始化、类型增强、语言资源）
- ❌ `utils/` / `helpers/` / `common/` 这类杂物抽屉包，**禁止新建**。真正的通用纯函数，前端进 `web/src/lib/`，后端就近放在使用它的包里，被 ≥2 个包依赖时才提独立包。

包内依赖方向必须单向，具体见各子规范。

### 1.3 反平铺规则（`make check-structure` 强制）

单个目录不应变成几十个文件的平面列表。阈值分两级，脚本执行：

- **硬线（fail）**：目录直接源文件 > **20 个**必须按相关性分包（`_test.go` 跟随实现不计；CLI 托管的 `web/src/components/ui/` 豁免）。
- **审视线（warn）**：> **8 个**时审视能否按功能域分组；同一功能域文件达 **3 个**即建子目录收拢。
- 现状即历史包袱的，**新文件按新规则放**，旧文件在顺路触碰时迁移，不专门发起大搬家。

### 1.4 文件拆分（`make check-structure` 强制）

一个文件只放一个主题。阈值分两级：

- **硬线（fail）**：任何源文件 > **800 行**必须拆分。
- **拆分信号（warn）**：Go > 400 行、TS/TSX > 300 行时审视拆分（`ui/` 与语言文件豁免软线）。
- 拆出去的文件必须有独立可命名的职责，拆出 `xxx-part2` 这种没有语义的碎片不如不拆。

### 1.5 工具包与索引（防重复造轮子）

可复用逻辑有唯一的家与唯一的索引，AI 协作者写逻辑前**必须先查索引**：

- **前端工具区**：纯函数进 `web/src/lib/`，React 逻辑进 `web/src/hooks/`；索引是 [web/src/lib/README.md](web/src/lib/README.md)。
- **后端工具区**：不设 utils 杂物包；通用函数就近放使用它的包里，被 ≥2 个包需要时提为具名叶子包；包地图与跨包工具索引是 [server/internal/README.md](server/internal/README.md)。
- 三条铁律：写逻辑前先查索引；≥2 处需要的逻辑必须进工具区；**动工具必须同步索引**（`check-structure` 对账，缺条目 fail）。

## 2. 跨端契约命名

前后端共同遵守的接口约定，改任何一侧都要对齐另一侧：

| 场景 | 规则 | 示例 |
| --- | --- | --- |
| REST 路径 | 复数资源名、kebab-case、嵌套表从属 | `/api/sessions/{id}/messages` |
| JSON 字段 | camelCase（Go struct tag 显式声明） | `pageSize`、`agentId` |
| SSE 事件 kind | snake_case | `message_chunk`、`turn_done` |
| 响应外壳 | `{"data": ...}` 或 `{"error": "..."}`，列表再包 `{items, total, page, pageSize}` | 见 README §API |
| git 分支 | `类型/短描述` | `feat/session-resume`、`fix/sse-dedup` |

领域类型以 `server/internal/model` 为源，`web/src/types/acp.ts` 与之字段对齐；改模型必须同步两处。

## 3. 辅助目录规范（docs / scripts / skills）

### 3.1 docs/ — 决策与专题文档

首次需要时创建。三个事实源各管一摊，内容放错位置比不写更糟：

- **怎么写代码** → AGENTS.md（各级）
- **系统现在是什么样**（架构、API、配置） → README
- **当初为什么这么定**（设计方案、决策记录、调研笔记） → `docs/`

规则：文件名 kebab-case；决策记录用 `adr-NNN-短题目.md` 编号递增，决策被推翻时不改旧文，新增一篇并互相链接；每篇在 README 或相关文档中有入口链接，不允许出现没人引用的孤儿文档。

### 3.2 scripts/ — 开发辅助脚本

首次需要时创建，收纳数据迁移、批量修复、抓取调试类脚本。规则：

- 文件头注释写清**用途、用法、前置条件**；不能安全重跑的脚本必须在头注释里显著标明。
- 命名 kebab-case，扩展名如实（`.sh` / `.mjs` / `.go`）。
- 不含密钥与硬编码环境路径，参数走环境变量或命令行。
- 会被日常使用的命令不留在 scripts/ 口口相传，挂进 Makefile 成为正式入口。
- 纯临时、用完即弃的脚本放系统临时目录，**不入库**。

### 3.3 .claude/skills/ — 工具知识库

- 目录内全部是第三方 skill，由 skills CLI 管理（`npx skills add/update`，锁文件是根目录 `skills-lock.json`）。**禁止手改**其中任何文件——升级会整体覆盖；增删改一律走 CLI，且改动必须连同 `skills-lock.json` 一起提交。
- skill 是 Claude Code 的按需加载机制，**Codex 不会自动读取**。因此人人必须遵守的规则永远写在 AGENTS.md，skill 里只放按需查阅的操作手册与长参考；需要时 AGENTS.md 可直接链接 skill 内的 md 文件（就是普通 markdown，Codex 也能当文档读）。
- 自建项目专属 skill 的门槛：同一工作流重复做过 ≥2 次、且步骤长到不适合塞进 AGENTS.md，才值得沉淀。结构照现有 skill：`<name>/SKILL.md` + frontmatter（`name`、`description`，description 写清触发场景），放 `.claude/skills/` 下并与第三方 skill 区分命名。
- 现有自建 skill：[skills-manage](.claude/skills/skills-manage/SKILL.md)——acpp 技能库的管理操作与 SKILL.md 写作规范，涉及技能管理功能（页面、API、注入）时先读它；[acp-debug](.claude/skills/acp-debug/SKILL.md)——ACP 协议排障手册（转录读法、probe、故障排查表）。Codex 都可直接当文档读。

## 4. 验证与提交

### 4.0 开发服务管理

- **端口是固定约定：后端 `127.0.0.1:48080`，前端 dev server `localhost:45173`**（特意选的不常用端口）。端口被占（包括其他会话留下的进程）一律 kill 掉旧进程后在原端口重启，**禁止**另起新端口跑第二套实例——vite proxy 硬编码 48080，多套实例会互相指错后端，还会留下一堆孤儿进程。
- **服务启停唯一入口是 `scripts/dev.sh`**（Makefile 已挂：`make dev` 重启全部 / `make stop` / `make status`）。`restart` 会先重新编译后端再启动，即「重启就是更新」；启动前自动清掉端口上的旧进程。日志在 `${TMPDIR:-/tmp}/acpp-dev/`。
- `make dev-server` / `make dev-web` 保留为**前台**运行方式（要盯实时日志时用）；后台常驻一律走脚本，不要手写 `nohup`/`&` 散装命令。

### 4.1 验证清单

提交前在仓库根目录跑一条命令：

```bash
make check       # lint + typecheck + test + check-structure，全绿才算过
```

改动小且明确只涉及一端时，可单跑 `make lint` / `make typecheck`（tsc -b）/ `make test` / `make check-structure`。改了前端构建相关配置（vite / tsconfig / 依赖）再跑 `make build-web` 确认能出产物。

### 4.2 提交规范

- Conventional Commits，描述用中文：`feat: 支持会话恢复`、`fix: SSE 重连后事件去重`。type 常用 `feat` / `fix` / `refactor` / `docs` / `chore` / `test`。
- 一个提交一个主题；机器生成的变更（`package-lock.json`、shadcn 覆盖）与手写逻辑尽量分开提交。
- **随做随提交（对 AI 协作者是硬性要求）**：每完成一个独立功能/修复，跑完验证就立即提交，再开始下一个。禁止攒一批功能打包成大杂烩 commit——「文件会交叉」不是攒批的理由，恰恰说明该在交叉发生前提交。节奏固定为：实现 → 验证 → commit → 下一个。跨功能的重构先单独提交。
- **禁止提交**：`server/data/`（本地库与 transcripts）、`web/dist/`、`server/bin/`、任何密钥或 token。
- 改动了以下内容必须同步 README：API 端点、SSE 事件 kind、环境变量、目录结构、数据模型。

## 5. AI 协作者须知

本项目的日常开发全部由 AI 协作者（Claude Code、Codex）完成，规范就是为此设计的：规则可执行、验证可一键、越界有脚本兜底。

- Claude 通过各级 `CLAUDE.md` 引用同级 `AGENTS.md`，Codex 直接读 `AGENTS.md`——**每份规范只维护 AGENTS.md 这一份**，不要往 CLAUDE.md 里写内容。深层规范同理：`server/internal/acp/`、`web/src/components/ui/` 各有一份目录级 AGENTS.md，动那里先读。
- 会话开始时先读 README 的架构与数据流小节，再定位代码。README 中「尚未实现」一节列出了已知空白，别把占位页当成 bug。
- 拿不准的产品决策（新增依赖、改 API 形状、改数据模型、跨层重构）：停下来向用户确认，不自作主张。
- 大改动前先陈述计划（动哪些文件、为什么），小改动直接做。
- 完成任务的定义：代码 + 测试 + 验证通过 + 文档同步，四者齐了才算完。
