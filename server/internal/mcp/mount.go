package mcp

import "maps"

// MergeClaudeMounts 合并 claude 侧的 _meta 挂载片段。
//
// claude 的形状是嵌套 map：`claudeCode.options.{mcpServers, allowedTools}`。
// 每个工具面都往这里塞自己那份，**直接覆盖会让后来的顶掉前面的**——数据库
// 工具面和报告工具面只能活一个，而且症状是「模型看不见某组工具」这种很难
// 追的静默失败。
//
// 只按这一个已知形状逐层合并，不做通用深合并：形状是我们自己定的，写死
// 比递归好读，也不会在遇到意外结构时悄悄合出个四不像。
// 住在本包：会话侧（service）与 discord 侧都要用，两个业务包不互相 import。
func MergeClaudeMounts(dst, src map[string]any) map[string]any {
	if len(src) == 0 {
		return dst
	}
	// 第一份直接接管：每个工具面返回的都是现构造的 map，没有别人再持有它。
	if dst == nil {
		return src
	}

	dstOpts, ok1 := claudeOptions(dst)
	srcOpts, ok2 := claudeOptions(src)
	if !ok1 || !ok2 {
		// 形状不认识时保守处理：只补 dst 没有的顶层键，绝不覆盖。
		for k, v := range src {
			if _, exists := dst[k]; !exists {
				dst[k] = v
			}
		}
		return dst
	}

	if sm, ok := srcOpts["mcpServers"].(map[string]any); ok && len(sm) > 0 {
		dm, _ := dstOpts["mcpServers"].(map[string]any)
		if dm == nil {
			dm = map[string]any{}
			dstOpts["mcpServers"] = dm
		}
		maps.Copy(dm, sm)
	}
	if st, ok := srcOpts["allowedTools"].([]string); ok && len(st) > 0 {
		dt, _ := dstOpts["allowedTools"].([]string)
		dstOpts["allowedTools"] = append(dt, st...)
	}
	return dst
}

// claudeOptions 取出 `claudeCode.options` 那一层，形状对不上就报 false。
func claudeOptions(meta map[string]any) (map[string]any, bool) {
	cc, ok := meta["claudeCode"].(map[string]any)
	if !ok {
		return nil, false
	}
	opts, ok := cc["options"].(map[string]any)
	return opts, ok
}
