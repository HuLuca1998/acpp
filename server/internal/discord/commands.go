package discord

import "strings"

// 斜杠命令的**唯一事实源**：Discord 注册、频道使用手册、/help 的命令
// 清单全部从这张表渲染。加/改命令只动这里，三处输出自动跟上——教训是
// 真实的：加了 /skills /usage 之后手册和 /help 都没人记得补（用户点名），
// 手工维护三份清单必然漂移。
//
// options 放静态参数定义；/model 的 choices 依赖 catalog 快照，由
// registerCommands 在注册时动态注入（见那里的深拷贝说明）。

type slashCommand struct {
	name string
	// desc 是 Discord 命令菜单里的描述（≤100 字符）。
	desc string
	// hint 是手册与 /help 命令清单里的短说明（两五个字）。
	hint    string
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
		{name: "init", desc: "把这个频道绑定到一个 git 仓库工作区", hint: "绑定频道"},
		{name: "help", desc: "acpp 使用指南", hint: "用法速览"},
		{name: "status", desc: "查看这个频道的工作区绑定", hint: "看绑定"},
		{name: "model", desc: "切换这个频道用的模型", hint: "换模型",
			options: []map[string]any{{
				"type": 3, "name": "model", "description": "要切换到的模型", "required": true,
			}}},
		{name: "effort", desc: "切换这个频道的思考深度", hint: "调思考深度",
			options: []map[string]any{{
				"type": 3, "name": "effort", "description": "思考深度档位",
				"required": true, "choices": effortChoicesJSON,
			}}},
		{name: "access", desc: "切换这个频道的安全权限档", hint: "调权限档",
			options: []map[string]any{{
				"type": 3, "name": "access", "description": "权限档位",
				"required": true, "choices": accessChoicesJSON,
			}}},
		{name: "db", desc: "数据库：子区工具面开关 + 本频道锁定哪个库", hint: "数据库",
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
		{name: "skills", desc: "列出注入对话的技能", hint: "看技能"},
		{name: "usage", desc: "本子区的用量统计（回合 / 工具 / token）", hint: "看用量"},
		{name: "mcps", desc: "本子区挂载的 MCP 工具面", hint: "看工具面"},
		{name: "stop", desc: "中止子区里正在跑的回合", hint: "中止本轮"},
		{name: "unbind", desc: "解绑这个频道的工作区（磁盘克隆保留）", hint: "解绑"},
	}
}

// commandsLine 渲染命令清单一行（频道手册与 /help 共用）：
// `/init` 绑定频道 · `/help` 用法速览 · …
func commandsLine() string {
	var parts []string
	for _, c := range slashCommands() {
		parts = append(parts, "`/"+c.name+"` "+c.hint)
	}
	return strings.Join(parts, " · ")
}
