package webshot

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 真机集成测试：驱动本机 Chrome 渲染一页并校验 PNG 产物。没装 Chrome
// 的环境（CI）自动跳过——这个包的价值就在真渲染，mock 没有意义。
func TestCaptureIntegration(t *testing.T) {
	if !Available() {
		t.Skip("本机没有 Chrome")
	}
	dir := t.TempDir()
	page := filepath.Join(dir, "page.html")
	html := `<!doctype html><meta charset="utf-8"><title>t</title>
<body style="margin:0"><div style="width:100%;height:2400px;background:linear-gradient(#123,#eee)">
<h1>整页截图测试</h1></div>`
	if err := os.WriteFile(page, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	png, err := Capture(ctx, "file://"+page, 1100)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatalf("产物不是 PNG（前 8 字节 %x）", png[:min(8, len(png))])
	}
	// 2400px 高的页面 2x 截出来不该小于 50KB——太小说明只截了视口。
	if len(png) < 50_000 {
		t.Errorf("PNG 只有 %d bytes，疑似没截全页", len(png))
	}
}
