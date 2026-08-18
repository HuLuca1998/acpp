package httpapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 契约：尾随读到末尾是「暂时没有新内容」，不是错误。
//
// 日志面板每 2 秒轮询一次，这条判断走错一步就是控制台每分钟三十条红字，
// 真正的报错会被埋掉——这个坑踩过一次，测试把它钉住。
func TestTailState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "57.jsonl")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		rng  string
		path string
		want tailKind
	}{
		{"没有 Range 就是要全量", "", path, tailNormal},
		{"偏移在中间，有增量可读", "bytes=4-", path, tailNormal},
		{"偏移正好在末尾", "bytes=10-", path, tailEmpty},
		{"偏移超过末尾说明文件被重建", "bytes=99-", path, tailRewound},
		{"转录还没落盘", "bytes=0-", filepath.Join(dir, "nope.jsonl"), tailEmpty},
		// 认不出的 Range 交回 ServeFile 按标准处理，别自作主张。
		{"带结束位置的 Range 不归我们管", "bytes=0-5", path, tailNormal},
		{"后缀写法同理", "bytes=-5", path, tailNormal},
		{"不是 bytes 单位", "items=0-", path, tailNormal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/sessions/57/transcript", nil)
			if c.rng != "" {
				r.Header.Set("Range", c.rng)
			}
			if got := tailState(r, c.path); got != c.want {
				t.Errorf("tailState(%q) = %v，想要 %v", c.rng, got, c.want)
			}
		})
	}
}

func TestRangeStart(t *testing.T) {
	ok := map[string]int64{
		"bytes=0-":    0,
		"bytes=1024-": 1024,
		" bytes=42- ": 42,
	}
	for header, want := range ok {
		got, valid := rangeStart(header)
		if !valid || got != want {
			t.Errorf("rangeStart(%q) = %d,%v，想要 %d,true", header, got, valid, want)
		}
	}
	for _, header := range []string{"", "bytes=-5", "bytes=0-5", "items=0-", "bytes=abc-", "bytes=-1-"} {
		if _, valid := rangeStart(header); valid {
			t.Errorf("rangeStart(%q) 不该认", header)
		}
	}
}
