package gist

import (
	"testing"
	"time"
)

// 描述是这个包唯一的持久状态：归属、到期时刻、标题全靠它往返。写坏了
// 既有链接就再也认不出来（列不出、撤不掉、过期不清），所以这条往返测试
// 盯着格式本身。
func TestDescRoundTrip(t *testing.T) {
	exp := time.Date(2026, 9, 7, 10, 30, 0, 0, time.UTC)
	for _, c := range []struct {
		name    string
		title   string
		owner   string
		expires time.Time
	}{
		{"全字段", "pp-game 用户行为日报", "1543925836489429002", exp},
		{"不过期", "架构梳理", "1543925836489429002", time.Time{}},
		{"无归属", "临时报告", "", exp},
		{"标题带空格与中文标点", "今日数据 · 汇总（prod）", "123", exp},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := parseDesc("abc123", buildDesc(c.title, c.owner, c.expires))
			if got.Title != c.title {
				t.Errorf("标题 = %q，想要 %q", got.Title, c.title)
			}
			if got.Owner != c.owner {
				t.Errorf("归属 = %q，想要 %q", got.Owner, c.owner)
			}
			if !got.ExpiresAt.Equal(c.expires) {
				t.Errorf("到期 = %v，想要 %v", got.ExpiresAt, c.expires)
			}
			if got.ViewURL != viewBase+"abc123" {
				t.Errorf("查看链接 = %q", got.ViewURL)
			}
		})
	}
}

// 认领闸：描述没有 acpp 前缀的一律不是我们的，List/Cleanup/Revoke 都靠它
// 把用户自己的 gist 挡在外面。
func TestDescPrefixGuard(t *testing.T) {
	if desc := buildDesc("x", "y", time.Time{}); desc[:len(descPrefix)] != descPrefix {
		t.Fatalf("发布出去的描述必须带认领前缀，得到 %q", desc)
	}
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if (Link{}).Expired(now) {
		t.Error("没有到期时刻 = 永不过期")
	}
	if !(Link{ExpiresAt: now.Add(-time.Second)}).Expired(now) {
		t.Error("到期时刻在过去 = 已过期")
	}
	if (Link{ExpiresAt: now.Add(time.Second)}).Expired(now) {
		t.Error("到期时刻在将来 = 未过期")
	}
}

func TestParseTTL(t *testing.T) {
	def := 7 * 24 * time.Hour
	for in, want := range map[string]time.Duration{
		"":      def,
		"7d":    7 * 24 * time.Hour,
		"30d":   30 * 24 * time.Hour,
		"12h":   12 * time.Hour,
		"90m":   90 * time.Minute,
		"never": 0,
		"永久":    0,
	} {
		got, err := ParseTTL(in, def)
		if err != nil || got != want {
			t.Errorf("ParseTTL(%q) = %v, %v；想要 %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"一周", "7 days", "-3d", "0d"} {
		if _, err := ParseTTL(bad, def); err == nil {
			t.Errorf("ParseTTL(%q) 应该报错", bad)
		}
	}
}

// gh 的进度行与结果链接在 stdout/stderr 之间挪过窝，所以取链接不认行只
// 认词，且要拿最后一个。
func TestLastURL(t *testing.T) {
	out := "- Creating gist report.html\n✓ Created secret gist report.html\nhttps://gist.github.com/u/abc123\n"
	if got := lastURL(out); got != "https://gist.github.com/u/abc123" {
		t.Errorf("lastURL = %q", got)
	}
	if got := lastURL("没有链接的输出"); got != "" {
		t.Errorf("没有链接时应给空串，得到 %q", got)
	}
}
