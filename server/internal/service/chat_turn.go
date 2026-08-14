package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/stream"
)

// DeriveTitle 从首条消息取首行并截短，作为会话的自动标题。
func DeriveTitle(text string) string {
	line := strings.TrimSpace(text)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	const maxRunes = 24
	runes := []rune(line)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return line
}

// ImageInput 是随消息上传的一张图片（base64，无 data: 前缀）。
type ImageInput struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// SendInput 是发一轮的入参：文本 + 可选图片 + 可选 @ 引用的文件路径。
type SendInput struct {
	Content string       `json:"content"`
	Images  []ImageInput `json:"images,omitempty"`
	// Files 是 @ 引用的文件（绝对路径或相对会话 cwd），内容由后端读出
	// 以 resource 块嵌进 prompt（两端 embeddedContext 都支持）。
	Files []string `json:"files,omitempty"`
}

// Send 广播用户消息并异步跑一轮。消息本身不落库——session/prompt 请求会
// 原样进转录，重建时从那里读回；这里广播的临时消息只为界面即时显示。
func (s *ChatService) Send(ctx context.Context, sessionID uint, in SendInput) (*model.Message, error) {
	text := in.Content
	if strings.TrimSpace(text) == "" && len(in.Images) == 0 && len(in.Files) == 0 {
		return nil, fmt.Errorf("%w: message content is required", ErrInvalid)
	}

	view, err := s.Open(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// 组 prompt 内容块：@ 文件 → resource（后端读内容），图片 → image，正文 → text。
	blocks, payload, err := s.buildBlocks(view.Cwd, in)
	if err != nil {
		return nil, err
	}

	// 无标题的会话用首条消息的简写自动命名（对齐主流 AI 聊天应用）。
	// 在返回 202 之前同步落库，前端跳转后立刻能看到新标题。
	if view.Title == "" {
		if title := DeriveTitle(text); title != "" {
			if err := s.db.WithContext(ctx).Model(&model.Session{}).
				Where("id = ?", sessionID).Update("title", title).Error; err != nil {
				slog.Warn("auto title", "session", sessionID, "err", err)
			}
		}
	}

	// 临时 id 用毫秒时间戳：远大于重建器的行号序 id，不会与其冲突；
	// turn 结束后前端重拉重建列表，这条临时消息随之被替换。
	msg := &model.Message{
		ID:        uint(time.Now().UnixMilli()),
		SessionID: sessionID,
		Role:      model.RoleUser,
		Kind:      model.KindText,
		Content:   text,
		Payload:   payload,
		CreatedAt: time.Now(),
	}

	br := s.brokerFor(sessionID)
	// 一轮进行中的插话（steering）并入当前轮，不能清当前轮的重放缓冲；
	// 只有真正开新轮才重置。
	if !s.manager.TurnActive(sessionKey(sessionID)) {
		br.StartTurn()
	}
	br.Publish(StreamEvent{Kind: "user_message", Message: msg})

	go s.runTurn(sessionID, br, blocks)

	return msg, nil
}

// buildBlocks 把发送入参翻译成 prompt 内容块（见 BuildPromptBlocks）。
func (s *ChatService) buildBlocks(cwd string, in SendInput) ([]acp.ContentBlock, model.JSONMap, error) {
	return BuildPromptBlocks(cwd, in)
}

// BuildPromptBlocks 把发送入参翻译成 prompt 内容块，并给临时消息组展示
// payload。@ 文件在这里读内容：路径相对会话 cwd 解析，读不到直接报错
// （发出去一个空引用比报错更糟）。编排会话的发送共用（导出）。
func BuildPromptBlocks(cwd string, in SendInput) ([]acp.ContentBlock, model.JSONMap, error) {
	var blocks []acp.ContentBlock
	payload := model.JSONMap{}

	var files []string
	for _, path := range in.Files {
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: read %s: %s", ErrInvalid, path, err)
		}
		if info.IsDir() {
			// 文件夹引用嵌「目录清单」而不是递归全文（token 灾难）：
			// agent 自有 fs 能力，给地图优于给全文（adr-002 §5.3）。
			blocks = append(blocks, acp.ResourceBlock("file://"+path+"/", DirReferenceListing(path)))
		} else {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: read %s: %s", ErrInvalid, path, err)
			}
			blocks = append(blocks, acp.ResourceBlock("file://"+path, string(data)))
		}
		files = append(files, path)
	}
	if len(files) > 0 {
		payload["files"] = files
	}

	var images []map[string]any
	for _, img := range in.Images {
		if img.Data == "" || img.MimeType == "" {
			return nil, nil, fmt.Errorf("%w: image data and mimeType are required", ErrInvalid)
		}
		blocks = append(blocks, acp.ImageBlock(img.Data, img.MimeType))
		images = append(images, map[string]any{"data": img.Data, "mimeType": img.MimeType})
	}
	if len(images) > 0 {
		payload["images"] = images
	}
	if strings.TrimSpace(in.Content) != "" {
		blocks = append(blocks, acp.TextBlock(in.Content))
	}
	if len(payload) == 0 {
		payload = nil
	}
	return blocks, payload, nil
}

// runTurn 跑完一轮。它在自己的 goroutine 里，
// 用 context.Background 是因为发起请求的 HTTP 连接早就返回了。
func (s *ChatService) runTurn(sessionID uint, br *stream.Broker, blocks []acp.ContentBlock) {
	ctx := context.Background()

	// active 的语义是「有一轮正在跑」，只在这里出现，结束时归 idle/error。
	if err := s.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ?", sessionID).Update("state", model.SessionActive).Error; err != nil {
		slog.Warn("mark session active", "session", sessionID, "err", err)
	}

	result, err := s.manager.Prompt(ctx, sessionKey(sessionID), blocks)
	if errors.Is(err, acp.ErrBusy) {
		// turn 进行中：改走插话通道（claude 排队为独立一轮，codex 注入当前轮）。
		var followUp bool
		result, followUp, err = s.manager.Interject(ctx, sessionKey(sessionID), blocks)
		if err == nil && !followUp {
			// 内容并入当前轮，收尾归当前轮的 runTurn，这里直接退场。
			return
		}
	}
	if err != nil {
		// 用户主动中止在 acp 层已归一成 StopCancelled 正常返回（Prompt 与
		// Interject 各自处理），走到这里的都是真实故障。
		br.Publish(StreamEvent{Kind: "error", Error: err.Error()})
		s.markSessionError(sessionID, err)
		// 与正常收尾同理：还有别的轮在途时不发 turn_done。
		if !s.manager.TurnActive(sessionKey(sessionID)) {
			br.EndTurn()
		}
		return
	}

	updates := map[string]any{"stop_reason": string(result.StopReason)}
	if result.StopReason.OK() {
		updates["state"] = model.SessionIdle
	} else {
		// 只有 end_turn 是正常说完；其余四种都意味着回答可能是残缺的。
		updates["state"] = model.SessionError
	}
	// 消息数在写路径上重建一次并缓存——列表读取绝不做全量重建。
	if all, err := s.rebuildAll(sessionID); err == nil {
		updates["message_count"] = len(all)
	}
	if err := s.db.WithContext(ctx).Model(&model.Session{}).Where("id = ?", sessionID).Updates(updates).Error; err != nil {
		slog.Error("save stop reason", "session", sessionID, "err", err)
	}

	// claude 的引导轮（promptQueueing）与原轮是两个并行的 runTurn：
	// 原轮先收尾时引导轮还在跑，此刻发 turn_done 会让前端误判「没有轮
	// 在跑」，把排队的插话 flush 进正在跑的轮、被二次 Interject 链式排队
	// （claude 消化不了第二层排队，消息就悬空无回复）。谁最后收尾谁发。
	if s.manager.TurnActive(sessionKey(sessionID)) {
		return
	}
	br.EndTurn()
}

// Cancel 中止会话上正在跑的一轮。
func (s *ChatService) Cancel(sessionID uint) error {
	if err := s.manager.Cancel(sessionKey(sessionID)); err != nil {
		return translateNoSession(sessionID, err)
	}
	return nil
}

// ResolvePermission 把用户对权限请求的裁决回给阻塞中的 agent。
// optionID 为空表示用户取消。
func (s *ChatService) ResolvePermission(sessionID uint, permissionID, optionID string) error {
	if err := s.manager.ResolvePermission(sessionKey(sessionID), permissionID, optionID); err != nil {
		return translateNoSession(sessionID, err)
	}
	return nil
}

// ResolveElicitation 把用户对交互式提问的作答回给阻塞中的 agent。
func (s *ChatService) ResolveElicitation(sessionID uint, elicitationID, action string, content map[string]any) error {
	switch action {
	case "accept", "decline", "cancel":
	default:
		return fmt.Errorf("%w: bad action %q", ErrInvalid, action)
	}
	err := s.manager.ResolveElicitation(sessionKey(sessionID), elicitationID, acp.ElicitationResult{
		Action:  action,
		Content: content,
	})
	if err != nil {
		return translateNoSession(sessionID, err)
	}
	return nil
}
