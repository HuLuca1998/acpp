package acp

import "strings"

// Runtime 描述怎么把一个 agent 拉起来。
type Runtime struct {
	Command string
	Args    []string
	// Env 是额外注入的环境变量。
	Env map[string]string
	// EnvRemove 是启动前必须从环境里摘掉的变量。
	EnvRemove []string
}

// nestedMarkers 是各 runtime 的嵌套会话标记。Claude Code 会给子进程注入
// CLAUDECODE 等变量，agent 继承后会误判自己跑在另一个 agent 内部而拒绝服务。
// 这个坑只在「从 agent 终端启动本服务」时复现，自己开发时多半碰不到。
var nestedMarkers = map[string][]string{
	"claude": {"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT"},
	"codex":  {"CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"},
}

// RuntimeFor 根据命令名推断该清理哪些嵌套标记。命令名认不出来时两组都清，
// 多删几个环境变量没有副作用，漏删会让 agent 直接拒绝服务。
func RuntimeFor(command string, args []string, env map[string]string) Runtime {
	rt := Runtime{Command: command, Args: args, Env: env}

	lower := strings.ToLower(command)
	matched := false
	for key, markers := range nestedMarkers {
		if strings.Contains(lower, key) {
			rt.EnvRemove = append(rt.EnvRemove, markers...)
			matched = true
		}
	}
	if !matched {
		for _, markers := range nestedMarkers {
			rt.EnvRemove = append(rt.EnvRemove, markers...)
		}
	}
	return rt
}

// buildEnv 在继承来的环境上摘掉嵌套标记、叠加自定义变量。
func (r Runtime) buildEnv(parent []string) []string {
	remove := make(map[string]bool, len(r.EnvRemove))
	for _, key := range r.EnvRemove {
		remove[key] = true
	}

	out := make([]string, 0, len(parent)+len(r.Env))
	for _, kv := range parent {
		key, _, ok := strings.Cut(kv, "=")
		if ok && remove[key] {
			continue
		}
		if _, overridden := r.Env[key]; overridden {
			continue
		}
		out = append(out, kv)
	}
	for key, value := range r.Env {
		out = append(out, key+"="+value)
	}
	return out
}
