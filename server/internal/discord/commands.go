package discord

// 斜杠命令的**唯一事实源**：Discord 注册、频道使用手册、/help 的命令
// 清单全部从这张表渲染。加/改命令只动这里，三处输出自动跟上——教训是
// 真实的：加了 /skills /usage 之后手册和 /help 都没人记得补（用户点名），
// 手工维护三份清单必然漂移。
//
// options 放静态参数定义；/model 的 choices 依赖 catalog 快照，由
// registerCommands 在注册时动态注入（见那里的深拷贝说明）。

type slashCommand struct {
	name string
	// desc 是 Discord 命令菜单里的描述（≤100 字符）。输入框打 `/` 时它就
	// 在命令旁边，是命令的**唯一**说明——手册与 /help 都不再抄一份清单，
	// 抄了必漂移（历史教训见 AGENTS.md）。
	desc    string
	options []map[string]any
}

// slashCommands 按「使用频率与生命周期」排序：绑定 → 日常 → 观察 → 收尾。
func slashCommands() []slashCommand {
	var effortChoicesJSON []map[string]any
	for _, c := range effortChoices() {
		effortChoicesJSON = append(effortChoicesJSON, map[string]any{
			"name": c.Label, "value": c.Value,
		})
	}
	var accessChoicesJSON []map[string]any
	for _, c := range accessChoices() {
		accessChoicesJSON = append(accessChoicesJSON, map[string]any{
			"name": c.Label + "——" + c.Description, "value": c.Value,
		})
	}
	return []slashCommand{
		{name: "init", desc: "绑定这个频道：选仓库、base 分支、模型、数据库和服务器"},
		{name: "help", desc: "用法速览：怎么开对话、能做什么、状态怎么看"},
		{name: "status", desc: "看绑了什么：仓库、分支、模型、权限、数据库"},
		{name: "model", desc: "换这个频道用的模型（claude / codex 各档）",
			options: []map[string]any{{
				"type": 3, "name": "model", "description": "要切换到的模型", "required": true,
			}}},
		{name: "effort", desc: "换思考深度：low / medium / high / xhigh / max",
			options: []map[string]any{{
				"type": 3, "name": "effort", "description": "思考深度档位",
				"required": true, "choices": effortChoicesJSON,
			}}},
		{name: "access", desc: "换权限档：安全（逐项确认）/ 自动编辑 / 完全放开",
			options: []map[string]any{{
				"type": 3, "name": "access", "description": "权限档位",
				"required": true, "choices": accessChoicesJSON,
			}}},
		{name: "server", desc: "换这个频道锁定的服务器（选「不锁定」解除）",
			options: []map[string]any{{
				// choices 由 registerCommands 按当前服务器清单动态注入。
				"type": 3, "name": "name", "description": "换绑本频道锁定的服务器",
				"required": false,
			}}},
		{name: "db", desc: "换这个频道锁定的库，或开关本子区的数据库工具",
			options: []map[string]any{{
				"type": 3, "name": "switch", "description": "on 挂载 / off 卸载（不填看状态）",
				"required": false, "choices": []map[string]any{
					{"name": "on", "value": "on"},
					{"name": "off", "value": "off"},
					{"name": "status", "value": "status"},
				},
			}, {
				// choices 由 registerCommands 按当前数据源清单动态注入。
				"type": 3, "name": "source", "description": "换绑本频道锁定的数据库（选「不锁定」解除）",
				"required": false,
			}}},
		{name: "git", desc: "看工作树：分支、与 base 的差距、改了哪些文件"},
		{name: "skills", desc: "看注入对话的技能清单"},
		{name: "usage", desc: "看本子区用量：回合数、工具调用、token"},
		{name: "mcps", desc: "看本子区挂载了哪些 MCP 工具面"},
		{name: "cron", desc: "定时任务：看清单、立即运行、停用/启用、删除、看运行记录",
			options: []map[string]any{{
				"type": 3, "name": "action", "description": "做什么（不填看清单）",
				"required": false, "choices": []map[string]any{
					{"name": "list——看本频道的定时任务", "value": "list"},
					{"name": "run——立即跑一次", "value": "run"},
					{"name": "pause——停用", "value": "pause"},
					{"name": "resume——启用", "value": "resume"},
					{"name": "runs——看最近的运行记录", "value": "runs"},
					{"name": "remove——删除（id 逗号分隔可删多条）", "value": "remove"},
					{"name": "clear——删光本频道的全部任务（要二次确认）", "value": "clear"},
				},
			}, {
				"type": 3, "name": "id", "description": "任务 id 或名字前缀；remove 可用逗号分隔多条（只有一条时可不填）",
				"required": false,
			}}},
		{name: "stop", desc: "中止子区里正在跑的这一轮"},
		{name: "unbind", desc: "解绑这个频道（没提交完的活会保留在磁盘上）"},
	}
}
