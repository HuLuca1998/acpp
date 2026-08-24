package acp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 回退（把会话上下文截断到某条消息）不是 ACP 标准能力。
//
// 协议里唯一沾边的是 session/fork，但它整段复制、没有截断参数；claude 与
// codex 的 sessionCapabilities 里也只有 claude 声明 fork。真正能截断的是
// claude 底层 SDK 的 `resumeSessionAt`——适配器不主动用，却把
// _meta.claudeCode.options 整体展开进 SDK options，于是客户端能借道用上
// （2026-08-24 实测可行，见 docs/adr-014）。
//
// 代价是这条路要跨进 claude 的私有存储：resumeSessionAt 只认它自己转录里
// 的条目 uuid，而 ACP 对外暴露的 messageId 是 Anthropic API 的 message id
// （msg_xxx）。两者的对照表只存在于 claude 的会话 JSONL 里，本文件负责查。

// ErrRewindUnavailable 表示这条会话没法做上下文回退——找不到 claude 的
// 转录、或那条消息不在里面。调用方据此降级为「原地重发」，不是故障。
var ErrRewindUnavailable = errors.New("acp: 该会话不支持上下文回退")

// RewindMetaExtra 组出让 claude 把上下文截断到 messageUUID 的 session/new
// 附加 _meta。两个字段都要给：resume 指定接哪条会话，resumeSessionAt 指定
// 截到哪一条为止（含该条）。
func RewindMetaExtra(acpSessionID, messageUUID string) map[string]any {
	return map[string]any{
		"claudeCode": map[string]any{
			"options": map[string]any{
				"resume":          acpSessionID,
				"resumeSessionAt": messageUUID,
			},
		},
	}
}

// ClaudeMessageUUID 把 ACP 给的 API message id 翻译成 claude 转录里的条目
// uuid。sessionID 是 ACP 会话 id（与 claude 自己的会话 id 是同一个值）。
func ClaudeMessageUUID(sessionID, apiMessageID string) (string, error) {
	if sessionID == "" || apiMessageID == "" {
		return "", ErrRewindUnavailable
	}
	path, err := claudeTranscriptPath(sessionID)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%w: 打开 claude 转录失败: %v", ErrRewindUnavailable, err)
	}
	defer f.Close()

	// 行可以很长（一条 assistant 消息含完整工具输入），默认 64KB 缓冲不够。
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var rec struct {
			UUID       string `json:"uuid"`
			IsAPIError bool   `json:"isApiErrorMessage"`
			Message    struct {
				ID string `json:"id"`
			} `json:"message"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue // 转录里混着 queue-operation 之类的非消息行，跳过即可
		}
		if rec.Message.ID != apiMessageID || rec.UUID == "" {
			continue
		}
		// API 报错（529 之类）也会作为 assistant 条目落进转录，id 是适配器
		// 自造的 uuid 而不是 msg_xxx。拿它当截断点 claude 会直接拒绝整条
		// session/new——那种轮次本来也没产出，没有上下文要丢，降级重发即可。
		if rec.IsAPIError {
			return "", fmt.Errorf("%w: %s 是一条 API 错误消息，不能当截断点",
				ErrRewindUnavailable, apiMessageID)
		}
		return rec.UUID, nil
	}
	return "", fmt.Errorf("%w: 转录里没有 %s", ErrRewindUnavailable, apiMessageID)
}

// claudeTranscriptPath 找出这条会话的 claude 转录文件。
//
// 目录名是把 cwd 的路径分隔符换成连字符得来的，但转义规则没有文档承诺
// （点号、下划线的处理都踩过），所以不去推算路径，直接按会话 id 的文件名
// 在所有项目目录里找——id 是 uuid，重名不可能。
func claudeTranscriptPath(sessionID string) (string, error) {
	// 会话 id 直接进路径，必须挡住跨目录穿越。
	if sessionID != filepath.Base(sessionID) || strings.ContainsAny(sessionID, `/\`) {
		return "", ErrRewindUnavailable
	}
	home := os.Getenv("CLAUDE_CONFIG_DIR")
	if home == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrRewindUnavailable, err)
		}
		home = filepath.Join(dir, ".claude")
	}
	matches, err := filepath.Glob(filepath.Join(home, "projects", "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("%w: 找不到 %s 的 claude 转录", ErrRewindUnavailable, sessionID)
	}
	return matches[0], nil
}
