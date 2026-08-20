package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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
	// 与 titler.MaxTitleRunes 同一个数：两种标题在同一处界面轮换出现，
	// 长度不一致会让侧边栏在标题升级的瞬间跳一下。
	const maxRunes = 15
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
	// DataSources 是 @ 引用的数据库（`<项目>/<环境>[/<库>[/<表>]]`），
	// 现状由后端查出后同样以 resource 块嵌入。与文件引用是同一个动作，
	// 只是内容来自库而不是磁盘。
	DataSources []string `json:"datasources,omitempty"`
}

// Send 广播用户消息并异步跑一轮。消息本身不落库——session/prompt 请求会
// 原样进转录，重建时从那里读回；这里广播的临时消息只为界面即时显示。
func (s *ChatService) Send(ctx context.Context, sessionID uint, in SendInput) (*model.Message, error) {
	text := in.Content
	if strings.TrimSpace(text) == "" && len(in.Images) == 0 && len(in.Files) == 0 &&
		len(in.DataSources) == 0 {
		return nil, fmt.Errorf("%w: message content is required", ErrInvalid)
	}

	view, err := s.Open(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// 组 prompt 内容块：@ 文件 → resource（后端读内容），图片 → image，正文 → text。
	blocks, payload, err := s.buildBlocks(ctx, view.Cwd, in)
	if err != nil {
		return nil, err
	}
	// 按 agent 声明的内容能力收口：规范禁止 client 越界发不支持的内容块。
	if settings, serr := s.manager.Settings(sessionKey(sessionID)); serr == nil {
		blocks, err = adaptBlocksToPromptCaps(blocks, settings.Prompt)
		if err != nil {
			return nil, err
		}
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

// buildBlocks 把发送入参翻译成 prompt 内容块（见 BuildPromptBlocks），
// 再补上 @ 数据库引用——那部分要现查库，纯函数拿不到数据源服务。
func (s *ChatService) buildBlocks(ctx context.Context, cwd string, in SendInput) ([]acp.ContentBlock, model.JSONMap, error) {
	blocks, payload, err := BuildPromptBlocks(cwd, in)
	if err != nil {
		return nil, nil, err
	}
	if len(in.DataSources) == 0 {
		return blocks, payload, nil
	}
	if s.sources == nil {
		return nil, nil, fmt.Errorf("%w: 数据库能力未启用", ErrInvalid)
	}
	refs, err := s.sources.Reference(ctx, cwd, in.DataSources)
	if err != nil {
		return nil, nil, err
	}

	blocks, payload = AppendDBReferences(blocks, payload, refs, strings.TrimSpace(in.Content) != "")
	return blocks, payload, nil
}

// AppendDBReferences 把展开好的数据库引用插进内容块。普通会话与编排共用。
//
// 插在正文之前而不是追加到末尾：BuildPromptBlocks 的约定是正文永远是
// 最后一块，先上下文后要求读起来才顺。
func AppendDBReferences(blocks []acp.ContentBlock, payload model.JSONMap,
	refs []DBReference, hasText bool) ([]acp.ContentBlock, model.JSONMap) {
	if len(refs) == 0 {
		return blocks, payload
	}

	refBlocks := make([]acp.ContentBlock, 0, len(refs))
	uris := make([]string, 0, len(refs))
	for _, ref := range refs {
		refBlocks = append(refBlocks, acp.ResourceBlock(ref.URI, ref.Text))
		uris = append(uris, ref.URI)
	}

	at := len(blocks)
	if hasText && at > 0 {
		at--
	}
	blocks = slices.Insert(blocks, at, refBlocks...)

	// payload 在没有任何附件时是 nil（BuildPromptBlocks 的收尾），
	// 往 nil map 里写会 panic。
	if payload == nil {
		payload = model.JSONMap{}
	}
	payload["datasources"] = uris
	return blocks, payload
}

// resourceLinkThreshold：超过它的 @ 文件不再全文内嵌（一次性吃掉几万
// token，agent 未必需要全文），改发 resource_link 让它按需自行读取
//（2026-08 实测两端都正确消化）。
const resourceLinkThreshold = 32 * 1024

// BuildPromptBlocks 把发送入参翻译成 prompt 内容块，并给临时消息组展示
// payload。@ 文件在这里读内容：路径相对会话 cwd 解析，读不到直接报错
// （发出去一个空引用比报错更糟）。编排会话的发送共用（导出）。
func BuildPromptBlocks(cwd string, in SendInput) ([]acp.ContentBlock, model.JSONMap, error) {
	var blocks []acp.ContentBlock
	payload := model.JSONMap{}

	var files []string
	var linked []string
	for _, path := range in.Files {
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: read %s: %s", ErrInvalid, path, err)
		}
		switch {
		case info.IsDir():
			// 文件夹引用嵌「目录清单」而不是递归全文（token 灾难）：
			// agent 自有 fs 能力，给地图优于给全文（adr-002 §5.3）。
			blocks = append(blocks, acp.ResourceBlock("file://"+path+"/", DirReferenceListing(path)))
		case info.Size() > resourceLinkThreshold:
			blocks = append(blocks, acp.ResourceLinkBlock("file://"+path, filepath.Base(path), info.Size()))
			linked = append(linked, path)
		default:
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
	if len(linked) > 0 {
		// 大文件按需读取的子集：气泡上的附件芯片据此换图标与提示。
		payload["linkedFiles"] = linked
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

// adaptBlocksToPromptCaps 把内容块收敛到 agent 声明的能力内（ACP 规定
// client 不得发送未声明支持的内容块）：图片不支持时直接报错——静默丢掉
// 用户刚传的图比报错更糟；内嵌上下文不支持时把 resource 降级成带来源头
// 的 text 块——text 永远合法，agent 照样拿到全部内容。已知方言（claude/
// codex）的能力由 adapter 兜底为 true，这里对它们是无操作。
func adaptBlocksToPromptCaps(blocks []acp.ContentBlock, p acp.PromptCapabilities) ([]acp.ContentBlock, error) {
	if p.Image && p.EmbeddedContext {
		return blocks, nil
	}
	out := make([]acp.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch {
		case b.Type == "image" && !p.Image:
			return nil, fmt.Errorf("%w: 该 agent 不支持图片输入", ErrInvalid)
		case b.Type == "resource" && !p.EmbeddedContext && b.Resource != nil:
			out = append(out, acp.TextBlock(b.Resource.URI+"\n"+b.Resource.Text))
		default:
			out = append(out, b)
		}
	}
	return out, nil
}

// runTurn 跑完一轮。它在自己的 goroutine 里，
// 用 context.Background 是因为发起请求的 HTTP 连接早就返回了。
func (s *ChatService) runTurn(sessionID uint, br *stream.Broker, blocks []acp.ContentBlock) {
	ctx := context.Background()
	// 用量快照落库放 defer：这一轮无论正常收尾、报错还是插话并入别的轮，
	// 都该把最近的水位留下——恰恰是失败那轮最值得知道「还剩多少余量」。
	defer s.saveUsageSnapshot(sessionID)

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
		// 标题升级借这次重建的结果，不再单独读一遍转录。异步是因为它要
		// 等外部模型，而这一轮的收尾不该被它拖住。
		go s.refineTitle(sessionID, br, all)
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

// ActiveTurnCount 数正在跑的轮——自更新前的「有人在干活」检查用。
func (s *ChatService) ActiveTurnCount() int {
	return s.manager.ActiveTurnCount()
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

// refineTitle 把首句派生的标题换成外部模型给的概括。
//
// 「该不该换」靠标题当前值自己回答：仍等于首句派生值就是还没被更好的
// 标题覆盖过。这样不必给会话加状态字段，而且天然幂等——AI 标题一旦落库，
// 之后每一轮都不再命中，符合「标题只在首轮定一次」。
//
// 全程失败无声：生成不出来时派生标题原样留着，界面没有任何异常。
func (s *ChatService) refineTitle(sessionID uint, br *stream.Broker, all []model.Message) {
	if s.titler == nil || !s.titler.Enabled() {
		return
	}
	user, agent := firstExchange(all)
	if user == "" {
		return
	}

	var cur string
	if err := s.db.Model(&model.Session{}).Where("id = ?", sessionID).
		Pluck("title", &cur).Error; err != nil {
		slog.Debug("refine title: 读当前标题失败", "session", sessionID, "err", err)
		return
	}
	if cur != DeriveTitle(user) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), titleTimeout)
	defer cancel()
	title, err := s.titler.Generate(ctx, user, agent)
	if err != nil {
		slog.Debug("refine title: 生成失败", "session", sessionID, "err", err)
		return
	}

	// 条件更新：这期间用户可能已经手动改了名，别把人家的标题盖掉。
	res := s.db.Model(&model.Session{}).
		Where("id = ? AND title = ?", sessionID, cur).Update("title", title)
	if res.Error != nil {
		slog.Warn("refine title: 落库失败", "session", sessionID, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		return
	}
	br.Publish(StreamEvent{Kind: "session_title", Title: title})
	slog.Info("会话标题已生成", "session", sessionID, "title", title)
}

// titleTimeout 给标题生成留够冷启动的时间：ollama 首次调用要把模型载进
// 内存，大模型十几秒是常态。它是异步的，等久点也不挡任何人。
const titleTimeout = 90 * time.Second

// firstExchange 取首轮的用户提问与 agent 回答正文。思考块与工具调用不进
// 标题素材——它们讲的是过程，标题要的是这轮在干什么。
func firstExchange(all []model.Message) (user, agent string) {
	for _, m := range all {
		if m.Kind != model.KindText || strings.TrimSpace(m.Content) == "" {
			continue
		}
		switch m.Role {
		case model.RoleUser:
			if user == "" {
				user = m.Content
			}
		case model.RoleAgent:
			if user != "" && agent == "" {
				agent = m.Content
			}
		}
		if user != "" && agent != "" {
			break
		}
	}
	return user, agent
}
