---
name: skills-manage
description: acpp 技能库管理手册。涉及本系统自管技能（~/.acpp 技能库与 skillpack 分发）时使用：(1) 创建/修改/启停/删除技能，(2) 按规范起草或优化 SKILL.md，(3) 开发调试技能管理页面、skills API 或会话注入，(4) 排查技能未被会话加载的问题。
---

# skills-manage

## 技能体系

acpp 会话对 skill 的策略是三层：**系统层替换、项目层照常、自带层不动**。

| 层 | 处置 |
| --- | --- |
| acpp 技能库（本手册管的） | 注入每条会话 |
| 目标项目（cwd 下的 `.claude/skills`、`.agents/skills` 等） | 照常加载 |
| 机器级（`~/.claude/skills`、`~/.codex/skills`） | 屏蔽 |
| runtime 内置（claude CLI 约 12 个、codex 内置 5 个 + ChatGPT 应用插件约 10 个） | 去不掉，视为地板 |

目录约定——源与分发分离：

```
~/.acpp/
├── skills/<name>/SKILL.md        # 源：全部技能（含停用），路径永远稳定
└── skillpack/                    # 分发：只放注入内容，别放数据库等任何其他东西
    ├── .claude-plugin/plugin.json               # {"name":"acpp"}，两端显示前缀 acpp:<name> 都取自它
    ├── skills/<name> -> ../../skills/<name>     # 启用 = 存在这条符号链接
    └── .agents/skills -> ./skills               # codex extraRoots 的固定入口
```

- **启用/停用 = 建/删 skillpack/skills/ 里的符号链接**，启用状态从文件系统读，不进数据库。
- skillpack 里不放分发内容以外的东西——codex 的 `additionalDirectories` 会把目录整个加进 sandbox 可写根，放了数据库就等于让 AI 能写库。
- 手工往 `~/.acpp/skills/` 放目录同样被识别；多文件技能（`references/`、`scripts/`）整目录分发。

## SKILL.md 写作规范

frontmatter 只写 `name` 与 `description`：

- `name` 与目录名一致，kebab-case。codex 的禁用选择器按 frontmatter name 全局匹配，起名避免与常见项目技能撞名。
- **description 是唯一触发器**——正文在触发后才加载，「何时用」写在正文里等于没写。description 必须包含：做什么 + 枚举触发场景。

正文规则：

- 只写模型不知道的东西：本域约定、协议行为、踩过的坑、项目私有知识。通用编程常识与自检提醒（「完成后请检查」）一律不写——模型自带，写了只烧上下文。
- 命令式语气；规则带理由（「不要 X——因为 Y」比裸禁令泛化得好）；不堆砌强调，「CRITICAL / MUST ALWAYS」会导致过度触发。
- 正文控制在 500 行内，超出就拆 `references/` 并在 SKILL.md 里写明每个文件何时读；引用只嵌一层，全部直接从 SKILL.md 链出。
- 资源分工：`scripts/` 放需要确定性的可执行操作，`references/` 放按需查阅的长参考，`assets/` 放产出物模板。不建 README、CHANGELOG 等旁支文件。

## 脚本头部规范（scripts/）

技能脚本头部用注释键值声明元信息，管理页面据此渲染参数控件并支持传参试运行。只认文件开头连续注释行（`#` 或 `//`），遇到第一个非注释行停止扫描：

```python
#!/usr/bin/env python3
# desc: 校验 SKILL.md frontmatter 是否符合规范
# usage: validate.py <skill-dir> [--strict]
# arg: skill-dir 技能目录路径
# opt: strict 严格模式（勾选后以 --strict 传入）
# env: ACPP_DEBUG 打开调试输出
```

- `desc:` 一句话用途；`usage:` 自由格式用法行，页面原样展示。
- `arg:` 位置参数，格式 `名字 描述`，按声明顺序传入；`opt:` 布尔开关，勾选后以 `--名字` 传入；`env:` 环境变量。
- 可运行的扩展名：`.py`（python3）、`.sh`（bash）、`.mjs` / `.js`（node）——按扩展名选解释器，不猜 shebang；其余扩展名只展示不给运行。
- 试运行以技能目录为 cwd，60 秒超时，输出各 256KB 截断；非零退出码是结果不是错误。

## 管理操作

- **创建**：`~/.acpp/skills/<name>/SKILL.md`，校验 frontmatter（name 存在且与目录一致、description 非空且含触发场景），默认建启用链接。
- **修改**：直接编辑源目录里的 SKILL.md 全文（含 frontmatter），不做字段化拆解——与手工编辑等价。
- **启停**：只动 skillpack 里的符号链接，源目录不动。
- **删除**：先删分发链接，再删源目录。
- **生效时机**：一切变更只影响新会话——agent 在 session/new 时读取一次，进行中的会话不重载。界面与操作反馈里都要讲清这点。

## 会话注入速查

已落地在 `server/internal/acp/isolation.go`(各 adapter 的 `Isolation`)+ `manager.go`(spawn 前算注入、session/new 与 session/load 都带)。Manager 构造注入 `<dataDir>/skillpack`,为空则不隔离。隔离手段只有两类载体：spawn 时的进程环境变量 + `session/new`/`session/load` 协议参数。不写 `~/.claude`、`~/.codex` 一个字节。

| | claude（claude-agent-acp） | codex（codex-acp） |
| --- | --- | --- |
| 注入 skillpack | `_meta.claudeCode.options.plugins: [{type:"local", path:<skillpack>}]` | `<codex-home>/skills` 软链到 `<skillpack>/skills`（codex 从家目录发现技能） |
| 屏蔽机器级 | `settingSources: ["project"]`——不开 user 档 | 环境变量 `CODEX_HOME=<dataDir>/codex-home`——机器级 `~/.codex/skills` 换掉家目录后彻底不在视野，连 `/skills` 都不列 |
| 保留项目级 | project 档保住 cwd 的 `.claude/skills` | cwd 进 `additionalDirectories` |
| 附带 | `strictMcpConfig: true` + env `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` | `<codex-home>` 里 auth.json 软链系统的、config.toml 复制系统副本（见 isolation.go ensureCodexHome） |

`_meta` 会整体覆盖 adapter 硬编码的 settingSources，这是 claude 侧唯一注入口。

codex 曾用 `CODEX_CONFIG` 的 `skills.config enabled:false` 逐个禁用机器级——但那只挡「模型能否加载使用」，**不挡显示**：被禁的技能仍出现在 `/skills` 与 `available_commands`（反映「发现」不反映「启用」）。要连显示都干净，改用 `CODEX_HOME` 整体重定向（2026-08-13 实测：机器级从命令列表彻底消失，codex 往新 home 只写几 MB，二重软链 `codex-home/skills → skillpack/skills/<name> → ../../skills/<name>` codex 能跟随）。claude 的 `available_commands` 本就反映真实启用，无此问题。两端实测版本：claude-agent-acp 0.63.0 / codex-acp 1.1.7 / codex 0.145.0。

## 验证

- 会话内：对 claude 发 `/context`、对 codex 发 `/skills`，都在斜杠命令清单里。
- 零模型开销：读 `available_commands_update`——claude 的列表反映真实启用，可做断言；codex 的只反映「发现」，被禁用的仍在列表里，是否真隔离要看模型自报。
- 全量验证：baseline 与隔离会话各建一条对比命令清单，再让模型自报技能清单；放一个带暗号的标记技能最直观。

实现进度见 README「尚未实现」一节——管理页面与注入落地前，本手册同时是实现要遵守的规范。
