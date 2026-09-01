package remote

import (
	"bytes"
	"strings"
	"testing"
)

// 契约：shellQuote 是本功能唯一的注入面——路径与 pattern 全是模型给的，
// 拼进命令行前必须让 shell 只当它是一个字面量参数。
func TestShellQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"普通路径", "/srv/pp-game/log", "'/srv/pp-game/log'"},
		{"带空格", "/srv/my app", "'/srv/my app'"},
		{"单引号", "it's", `'it'\''s'`},
		{"分号注入", "/tmp; rm -rf /", "'/tmp; rm -rf /'"},
		{"反引号与美元", "`whoami` $HOME", "'`whoami` $HOME'"},
		{"管道与重定向", "a | b > c", "'a | b > c'"},
		{"换行", "a\nb", "'a\nb'"},
		{"空串", "", "''"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("%s: shellQuote(%q) = %q, 期望 %q", tt.name, tt.in, got, tt.want)
		}
	}
}

// 契约：引号里的内容永远闭合。构造出来的命令行必须有偶数个未转义单引号，
// 否则后面的部分会被当成引号内的文本（或者更糟，跑出引号外）。
func TestShellQuote_AlwaysBalanced(t *testing.T) {
	nasty := []string{
		"'", "''", "'''", "a'b", "'; whoami; '", `\'`, "$(id)", "&& reboot",
	}
	for _, in := range nasty {
		q := shellQuote(in)
		if !strings.HasPrefix(q, "'") || !strings.HasSuffix(q, "'") {
			t.Errorf("shellQuote(%q) = %q 没有被引号包住", in, q)
		}
		// 单引号字面量里不允许出现裸的 '，只允许 '\'' 这个序列。
		inner := q[1 : len(q)-1]
		if strings.Contains(strings.ReplaceAll(inner, `'\''`, ""), "'") {
			t.Errorf("shellQuote(%q) = %q 里有没转义干净的单引号", in, q)
		}
	}
}

// 契约：多个参数各自独立引用，不会互相污染。
func TestQuoteAll(t *testing.T) {
	got := quoteAll("/var/log", "*.log", "a b")
	want := `'/var/log' '*.log' 'a b'`
	if got != want {
		t.Errorf("quoteAll = %q, 期望 %q", got, want)
	}
}

// 契约：输出预算是硬的。命令自身限流失效时（对端不认 head -c 之类），
// 这一层要兜住，不能让几百 MB 的日志灌进上下文。
func TestLimitWriter(t *testing.T) {
	var buf bytes.Buffer
	w := &limitWriter{w: &buf, n: 10}

	// 分多次写，总量超过预算。
	for range 5 {
		n, err := w.Write([]byte("abcde"))
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		// 必须**报告全部写入**：底层 ssh 会话把短写当错误处理。
		if n != 5 {
			t.Fatalf("应报告写入 5 字节（短写会被当成错误），得到 %d", n)
		}
	}
	if buf.Len() != 10 {
		t.Fatalf("最多留 10 字节，实际 %d：%q", buf.Len(), buf.String())
	}
	if buf.String() != "abcdeabcde" {
		t.Fatalf("留下的应是最前面的内容，得到 %q", buf.String())
	}
}

// 契约：剥掉 ANSI 转义。服务端日志普遍带颜色（zap 的 console 编码器默认
// 给级别上色），留着既占 token，又让 `error` 变成 `\x1b[31merror\x1b[0m`
// 这种模型搜不到的形状。
func TestAnsiStrip(t *testing.T) {
	tests := []struct{ in, want string }{
		{"\x1b[31merror\x1b[0m 出错了", "error 出错了"},
		{"\x1b[1;32mOK\x1b[0m", "OK"},
		{"没有颜色的行", "没有颜色的行"},
		{"\x1b[2J\x1b[H清屏", "清屏"},
	}
	for _, tt := range tests {
		if got := ansiRe.ReplaceAllString(tt.in, ""); got != tt.want {
			t.Errorf("剥 ANSI: %q → %q，期望 %q", tt.in, got, tt.want)
		}
	}
}
