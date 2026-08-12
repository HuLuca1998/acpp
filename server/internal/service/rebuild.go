package service

import (
	"encoding/json"
	"strings"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
	"acpp/server/internal/transcript"
)

// wireMsg 是转录里一行 JSON-RPC 消息的最小解码形态。
type wireMsg struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// rebuildTurn 攒住一轮里的正文、思考与工具调用。
type rebuildTurn struct {
	message   []byte
	thought   []byte
	toolOrder []string
	tools     map[string]*rebuildTool
}

type rebuildTool struct {
	title     string
	kind      string
	status    string
	rawInput  json.RawMessage
	rawOutput json.RawMessage
	content   json.RawMessage
}

// RebuildMessages 把线级转录重建成 UI 消息列表。
//
// 规则与旧的落库行为一致：session/prompt 请求产出 user 消息；
// 一轮内的 update 分片累积，prompt 响应到达时按 思考 → 工具 → 正文 落成消息。
// 进行中的半轮不产出——那部分由前端的流式态渲染。
func RebuildMessages(sessionID uint, entries []transcript.Entry) []model.Message {
	var out []model.Message
	nextID := uint(1)
	emit := func(m model.Message, ts time.Time) {
		m.ID = nextID
		m.SessionID = sessionID
		m.CreatedAt = ts
		nextID++
		out = append(out, m)
	}

	// 我们发出的请求 id → method，用来认领响应。
	sentMethods := map[string]string{}
	// agent 的反向请求（elicitation/create）按 id 暂存，等我们的 response 配对。
	type agentReq struct {
		method string
		params json.RawMessage
	}
	agentReqs := map[string]agentReq{}
	var turn *rebuildTurn

	// prompt / steering 共用：把内容块拼成一条 user 消息。
	emitUserBlocks := func(prompt []acp.ContentBlock, ts time.Time) {
		var text []byte
		var images []map[string]any
		var files []string
		for _, block := range prompt {
			switch block.Type {
			case "image":
				// 图片原文在转录里，重建时装回 payload 供气泡显示。
				images = append(images, map[string]any{
					"data": block.Data, "mimeType": block.MimeType,
				})
			case "resource":
				if block.Resource != nil {
					files = append(files, block.Resource.URI)
				}
			default:
				text = append(text, block.Text...)
			}
		}
		content := trimmed(text)
		if content == "" && len(images) == 0 && len(files) == 0 {
			return
		}
		payload := model.JSONMap{}
		if len(images) > 0 {
			payload["images"] = images
		}
		if len(files) > 0 {
			payload["files"] = files
		}
		if len(payload) == 0 {
			payload = nil
		}
		emit(model.Message{Role: model.RoleUser, Kind: model.KindText, Content: content, Payload: payload}, ts)
	}

	flush := func(ts time.Time, stopReason string) {
		if turn == nil {
			return
		}
		if thought := trimmed(turn.thought); thought != "" {
			emit(model.Message{Role: model.RoleAgent, Kind: model.KindThought, Content: thought}, ts)
		}
		for _, id := range turn.toolOrder {
			tool := turn.tools[id]
			// 轮已收尾，没走到终态的工具必然被中止/丢弃——
			// 历史消息不能停留在「执行中」。
			if tool.status == "" || tool.status == "pending" || tool.status == "in_progress" {
				tool.status = "cancelled"
			}
			payload := model.JSONMap{"toolCallId": id, "status": tool.status}
			if tool.kind != "" {
				payload["kind"] = tool.kind
			}
			if len(tool.rawInput) > 0 {
				payload["rawInput"] = json.RawMessage(tool.rawInput)
			}
			if len(tool.rawOutput) > 0 {
				payload["rawOutput"] = json.RawMessage(tool.rawOutput)
			}
			if len(tool.content) > 0 {
				payload["content"] = json.RawMessage(tool.content)
			}
			emit(model.Message{
				Role:    model.RoleAgent,
				Kind:    model.KindToolCall,
				Content: tool.title,
				Payload: payload,
			}, ts)
		}
		if content := trimmed(turn.message); content != "" {
			msg := model.Message{Role: model.RoleAgent, Kind: model.KindText, Content: content}
			if stopReason != "" && stopReason != string(acp.StopEndTurn) {
				msg.Payload = model.JSONMap{"stopReason": stopReason}
			}
			emit(msg, ts)
		}
		turn = nil
	}

	for _, entry := range entries {
		var msg wireMsg
		if err := json.Unmarshal(entry.Msg, &msg); err != nil {
			continue
		}

		switch {
		case entry.Dir == "send" && msg.Method == "session/prompt":
			if len(msg.ID) > 0 {
				sentMethods[string(msg.ID)] = msg.Method
			}
			// 上一轮如果没等到响应（进程中途死了），先按无结论落掉。
			flush(entry.TS, "")

			var p acp.PromptParams
			if err := json.Unmarshal(msg.Params, &p); err == nil {
				emitUserBlocks(p.Prompt, entry.TS)
			}
			turn = &rebuildTurn{tools: map[string]*rebuildTool{}}

		case entry.Dir == "send" && msg.Method == "_session/steering":
			// 插话注入正在跑的轮（codex steering）：产出用户消息但不结轮，
			// 本轮的思考/工具/正文仍由这一轮的 prompt 响应统一收尾。
			var p acp.SteeringParams
			if err := json.Unmarshal(msg.Params, &p); err == nil {
				emitUserBlocks(p.Prompt, entry.TS)
			}

		case entry.Dir == "send" && msg.Method != "" && len(msg.ID) > 0:
			sentMethods[string(msg.ID)] = msg.Method

		case entry.Dir == "send" && msg.Method == "" && len(msg.ID) > 0:
			// 我们对 agent 反向请求的答复；elicitation 配对后落成一条问答消息。
			req, ok := agentReqs[string(msg.ID)]
			delete(agentReqs, string(msg.ID))
			if !ok || req.method != "elicitation/create" {
				continue
			}
			var p acp.ElicitationParams
			if err := json.Unmarshal(req.params, &p); err != nil {
				continue
			}
			var result acp.ElicitationResult
			_ = json.Unmarshal(msg.Result, &result)
			payload := model.JSONMap{"action": result.Action}
			if len(p.RequestedSchema) > 0 {
				payload["schema"] = json.RawMessage(p.RequestedSchema)
			}
			if len(result.Content) > 0 {
				payload["answers"] = result.Content
			}
			emit(model.Message{
				Role:    model.RoleAgent,
				Kind:    model.KindElicitation,
				Content: p.Message,
				Payload: payload,
			}, entry.TS)

		case entry.Dir == "recv" && msg.Method != "" && len(msg.ID) > 0:
			// agent 的反向请求（提问、权限），等我们的 response 再配对。
			agentReqs[string(msg.ID)] = agentReq{method: msg.Method, params: msg.Params}

		case entry.Dir == "recv" && msg.Method == "session/update":
			if turn == nil {
				continue
			}
			var n acp.SessionNotification
			if err := json.Unmarshal(msg.Params, &n); err != nil {
				continue
			}
			applyUpdate(turn, n.Update)

		case entry.Dir == "recv" && msg.Method == "" && len(msg.ID) > 0:
			method := sentMethods[string(msg.ID)]
			delete(sentMethods, string(msg.ID))
			if method != "session/prompt" {
				continue
			}
			var result acp.PromptResult
			_ = json.Unmarshal(msg.Result, &result)
			flush(entry.TS, string(result.StopReason))
		}
	}

	return out
}

func applyUpdate(turn *rebuildTurn, u acp.SessionUpdate) {
	switch u.SessionUpdate {
	case acp.UpdateAgentMessageChunk:
		turn.message = append(turn.message, u.Text()...)

	case acp.UpdateAgentThoughtChunk:
		turn.thought = append(turn.thought, u.Text()...)

	case acp.UpdateToolCall, acp.UpdateToolCallUpdate:
		if u.ToolCallID == "" {
			return
		}
		tool, ok := turn.tools[u.ToolCallID]
		if !ok {
			tool = &rebuildTool{}
			turn.tools[u.ToolCallID] = tool
			turn.toolOrder = append(turn.toolOrder, u.ToolCallID)
		}
		// 更新只带变了的字段，空值不能覆盖已有内容。
		if u.Title != "" {
			tool.title = u.Title
		}
		if u.Kind != "" {
			tool.kind = u.Kind
		}
		if u.Status != "" {
			tool.status = u.Status
		}
		if len(u.RawInput) > 0 {
			tool.rawInput = u.RawInput
		}
		if len(u.RawOutput) > 0 {
			tool.rawOutput = u.RawOutput
		}
		if len(u.Content) > 0 {
			tool.content = u.Content
		}
	}
}

func trimmed(b []byte) string {
	return strings.TrimSpace(string(b))
}
