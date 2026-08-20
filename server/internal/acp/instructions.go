package acp

// 会话基础提示词：每条会话都注入的一段行为约定，与技能隔离同一批注入口
// （claude 走 session/new 的 _meta.systemPrompt，codex 走 CODEX_HOME 的
// AGENTS.md——它没有协议注入口，实测这是唯一稳定生效的口子）。
//
// 这里只放**与具体项目无关**的通用约定。按项目/按引用才成立的内容（数据库
// 数据源说明就是）不进来：常驻上下文是所有会话都要付的税，一段用不上的说明
// 既占注意力，又会诱导 AI 去碰用户这轮根本没提的东西。

// instructionsCore 是两端共用的正文：让 AI 主动拆任务、真的用工具维护清单。
//
// 写得这么具体是被实测逼的：只说「多步任务先建清单」时，模型照旧直接开干，
// 或者在正文里手写 markdown 复选框冒充进度（界面上的计划卡只认工具事件，
// 手写的它看不见）。所以阈值要可判定（两步以上）、动作要分条、豁免口要窄。
const instructionsCore = `# 任务清单

只要这次请求两步以上才能答完——要读多个文件、跑多条命令，或者答案分成几块——先建待办清单再动手，不要边想边做。

- 动手前把步骤列出来，一步一句话，通常不超过 6 步；
- 开始做某一步之前把它标成进行中，做完立刻标完成，不要攒到最后一次性勾完；
- 中途发现漏了步骤就补进清单。

清单必须用**工具**建和更新：用户界面上的进度卡只认工具事件，在正文里手写 markdown 复选框等于没建。

只有一句话就能答完的问题才免建清单。`

// claudeToolHint 补上 claude 这边的现实：待办工具是**延迟加载**的。
//
// 实测（2026-08-20，claude-agent-acp 0.63.0）：会话开场 TodoWrite 根本不存在，
// Task* 六件套只在 deferred 名单里有名字、没有 schema，不先检索就调不动；
// 而 SDK 没有关掉延迟加载的开关——tools 显式数组会整体替换工具集（太脆），
// allowedTools 列进去也不会加载 schema（实测无效）。所以只能教它自己去取。
const claudeToolHint = `

待办清单工具（TaskCreate / TaskUpdate / TaskList）是延迟加载的，会话开场不在你的工具列表里。需要建清单时先用 ToolSearch 检索 ` + "`select:TaskCreate,TaskUpdate,TaskList`" + ` 把它们取回来——这一步很便宜，别因为嫌麻烦就跳过、改用手写清单。`

// codexToolHint：codex 的计划工具是原生的，不用检索，但它的清单每轮独立，
// 要提醒跨轮任务重新列，否则上一轮的进度看着像丢了。
const codexToolHint = `

你的计划工具（update_plan）随时可用，不需要检索。注意计划是**每轮独立**的：跨轮继续同一件事时，把还没做完的步骤重新列一遍，别默认用户还看得见上一轮的清单。`

// ClaudeInstructions 是注入 claude 会话的基础约定。
func ClaudeInstructions() string { return instructionsCore + claudeToolHint }

// CodexInstructions 是注入 codex 会话的基础约定。
func CodexInstructions() string { return instructionsCore + codexToolHint }
