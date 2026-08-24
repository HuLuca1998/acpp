package acp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// claudeHome 摆一份 claude 的会话转录：条目自带 uuid，assistant 条目另有
// message.id（Anthropic API 的 id）——这正是回退要查的对照表。
func claudeHome(t *testing.T, sessionID string, lines ...string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "projects", "-Users-luca-work-acpp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", home)
}

// ACP 只给 API message id，resumeSessionAt 只认转录里的 uuid——
// 这个翻译对不上，上下文回退就无从谈起。
func TestClaudeMessageUUID(t *testing.T) {
	const sid = "00af3912-a565-4df2-9348-02daae8473e5"
	claudeHome(t, sid,
		`{"type":"queue-operation"}`,
		`{"type":"user","uuid":"6c3bf38f-34e3-45f3-8c18-8aabd89f233f","message":{"role":"user","content":"记住 ALPHA"}}`,
		`{"type":"assistant","uuid":"e45499da-ade1-4720-b9f7-5716512717d2","message":{"id":"msg_011CeMAjT2cgbdb5WD5Eyk3U","content":[{"type":"text","text":"记住了"}]}}`,
		`{"type":"assistant","uuid":"e544b52f-7f5a-4a1d-91ac-bb5cc9d2ec54","message":{"id":"msg_011CeMAjbLS5JWsuj3FRztgh","content":[{"type":"text","text":"好"}]}}`,
	)

	t.Run("按 API id 翻出 uuid", func(t *testing.T) {
		got, err := ClaudeMessageUUID(sid, "msg_011CeMAjT2cgbdb5WD5Eyk3U")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got != "e45499da-ade1-4720-b9f7-5716512717d2" {
			t.Errorf("uuid = %q，期望第一条 assistant 的 uuid", got)
		}
	})

	t.Run("转录里没有那条消息", func(t *testing.T) {
		_, err := ClaudeMessageUUID(sid, "msg_不存在")
		if !errors.Is(err, ErrRewindUnavailable) {
			t.Errorf("err = %v，期望 ErrRewindUnavailable（调用方据此降级重发）", err)
		}
	})

	t.Run("没有这条会话的转录", func(t *testing.T) {
		// codex 会话就是这个样子：根本不存在 claude 的转录文件。
		_, err := ClaudeMessageUUID("11111111-2222-3333-4444-555555555555", "msg_x")
		if !errors.Is(err, ErrRewindUnavailable) {
			t.Errorf("err = %v，期望 ErrRewindUnavailable", err)
		}
	})

	t.Run("会话 id 不许穿越目录", func(t *testing.T) {
		_, err := ClaudeMessageUUID("../../etc/passwd", "msg_x")
		if !errors.Is(err, ErrRewindUnavailable) {
			t.Errorf("err = %v，期望挡住", err)
		}
	})
}

// 回退要同时给 resume 与 resumeSessionAt：少一个 claude 就不知道接哪条会话、
// 截到哪为止（实测只给 resumeSessionAt 会报 message uuid 找不到）。
func TestRewindMetaExtra(t *testing.T) {
	meta := RewindMetaExtra("sess-1", "uuid-9")
	opts, ok := meta["claudeCode"].(map[string]any)["options"].(map[string]any)
	if !ok {
		t.Fatalf("meta 形状不对: %+v", meta)
	}
	if opts["resume"] != "sess-1" || opts["resumeSessionAt"] != "uuid-9" {
		t.Errorf("options = %+v，期望带上会话 id 与截断点", opts)
	}
}
