package titler

import "testing"

func TestSanitize(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"干净标题原样通过", "让AI总结会话标题", "让AI总结会话标题"},
		{"剥思考块", "<think>用户想要标题</think>修复登录超时", "修复登录超时"},
		{"未闭合思考块判废", "<think>我先想想该怎么起", ""},
		{"双引号包裹", `"排查SSE重连丢事件"`, "排查SSE重连丢事件"},
		{"中文引号包裹", "“排查SSE重连丢事件”", "排查SSE重连丢事件"},
		{"书名号包裹", "《配置本地模型》", "配置本地模型"},
		{"markdown加粗", "**接入ollama生成标题**", "接入ollama生成标题"},
		{"标题前缀", "标题：迁移数据目录", "迁移数据目录"},
		{"英文前缀大小写不敏感", "Title: migrate dir", "migrate dir"},
		{"列表编号", "1. 修复权限卡不弹", "修复权限卡不弹"},
		{"符号列表", "- 修复权限卡不弹", "修复权限卡不弹"},
		{"尾部句号", "创建只读数据库账号。", "创建只读数据库账号"},
		{"多层嵌套", "**标题：「接入本地模型」**。", "接入本地模型"},
		{"只取首行", "生成会话标题\n\n这个标题概括了整段对话的主题。", "生成会话标题"},
		{"内部空白压缩", "生成  Go   中间件", "生成 Go 中间件"},
		{"空输入", "", ""},
		{"纯空白", "   \n\t  ", ""},
		// 单边引号是内容的一部分——剥掉会让标题读不通。
		{"单边引号保留", `他说"跑不动`, `他说"跑不动`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sanitize(c.raw, 15); got != c.want {
				t.Errorf("Sanitize(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestSanitizeTruncates(t *testing.T) {
	// 15 字上限按字符算，不能按字节——中文一个字三字节，按字节截会切出乱码。
	const long = "分析SSE重连后事件重复消费的根本原因并给出修复方案"
	got := Sanitize(long, 15)
	if n := len([]rune(got)); n != 15 {
		t.Fatalf("截断后 %d 字，想要 15：%q", n, got)
	}
	if got != "分析SSE重连后事件重复消费的" {
		t.Errorf("截断结果 = %q", got)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatalf("截出了无效字符：%q", got)
		}
	}
}

func TestSanitizeTruncateDropsTrailingPunct(t *testing.T) {
	// 截断点正好落在标点上时，标点会露到末尾，必须再削一次。
	if got := Sanitize("已完成数据库连接配置，接下来做什么", 8); got != "已完成数据库连接" {
		t.Errorf("got %q", got)
	}
	if got := Sanitize("修复登录超时，验证通过", 6); got != "修复登录超时" {
		t.Errorf("got %q", got)
	}
}

func TestSanitizeUnlimited(t *testing.T) {
	// maxRunes<=0 表示不截断，净化规则照常生效。
	if got := Sanitize("**很长很长的标题**", 0); got != "很长很长的标题" {
		t.Errorf("got %q", got)
	}
}
