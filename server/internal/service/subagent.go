package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// subagentReadTimeout 兜住一次转录读取。子代理 thread 可能很长，但重放是
// 本地文件读，慢不到哪去；卡住多半是 agent 出了别的问题。
const subagentReadTimeout = 60 * time.Second

// SubagentOutput 读一个 codex 子代理的最终产出。
//
// codex 的子代理是独立 thread，主流里只有一条活动事件，产出不在协议里——
// 唯一的取法是拿它的 threadId 开一条一次性会话 session/load 出来。claude 不
// 需要这条路：它的 Agent 调用自带 rawOutput，界面直接就有。
//
// 子 thread fork 了父会话的全部历史，重放出来的前半截是父会话的旧内容。
// 取最后一条正文正好绕开——子代理自己的汇报必定排在最后。
func (s *ChatService) SubagentOutput(ctx context.Context, sessionID uint, threadID string) (string, error) {
	if strings.TrimSpace(threadID) == "" {
		return "", fmt.Errorf("subagent thread id is required: %w", ErrInvalid)
	}

	var session model.Session
	if err := s.db.WithContext(ctx).Preload("Agent").First(&session, sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("session %d: %w", sessionID, ErrNotFound)
		}
		return "", fmt.Errorf("load session %d: %w", sessionID, err)
	}
	if session.Agent == nil {
		return "", fmt.Errorf("session %d has no agent: %w", sessionID, ErrInvalid)
	}

	ctx, cancel := context.WithTimeout(ctx, subagentReadTimeout)
	defer cancel()

	// 读取会话不干活，cwd 用独立临时目录，别把子代理的历史往用户工作区里带。
	cwd := filepath.Join(os.TempDir(), "acpp-subagent")
	key := "subagent-" + threadID

	var mu sync.Mutex
	var texts []string
	_, err := s.manager.Open(ctx, acp.OpenOptions{
		Key:                key,
		Runtime:            acp.RuntimeFor(session.Agent.Command, session.Agent.Args, session.Agent.Env),
		Cwd:                cwd,
		ResumeACPSessionID: threadID,
		// 要的就是这条历史，别按常规会话的规矩把重放丢掉。
		ReplayEvents: true,
		OnEvent: func(ev acp.Event) {
			if ev.Kind != acp.EventMessage {
				return
			}
			if text := strings.TrimSpace(ev.Text); text != "" {
				mu.Lock()
				texts = append(texts, text)
				mu.Unlock()
			}
		},
	})
	if err != nil {
		return "", fmt.Errorf("load subagent thread %s: %w", threadID, err)
	}
	defer func() { _ = s.manager.Close(key) }()

	mu.Lock()
	defer mu.Unlock()
	if len(texts) == 0 {
		return "", nil
	}
	return texts[len(texts)-1], nil
}
