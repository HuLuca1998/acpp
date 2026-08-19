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

// rebuildTurn 按**时序**攒住一轮里的内容。agent 的节奏通常是「说一句 →
// 干点活 → 再说一句」，把整轮正文揉成一条消息会把这个节奏抹平：界面上
// 两次发言首尾相接，读起来像一句话说到一半突然换了话题。
type rebuildTurn struct {
	items []*turnItem
	tools map[string]*rebuildTool
}

// turnItem 是轮内时间线上的一段：一段连续的正文、一段连续的思考，
// 或一次工具调用。
type turnItem struct {
	kind model.MessageKind
	// buf 是正文/思考的分片累积，工具调用不用。
	buf    []byte
	toolID string
}

// appendChunk 把分片接到时间线末尾那段同类内容上；末尾不是同类
// （中间插了工具调用）就另起一段——那正是消息该断开的地方。
func (t *rebuildTurn) appendChunk(kind model.MessageKind, text string) {
	if text == "" {
		return
	}
	if n := len(t.items); n > 0 && t.items[n-1].kind == kind {
		t.items[n-1].buf = append(t.items[n-1].buf, text...)
		return
	}
	t.items = append(t.items, &turnItem{kind: kind, buf: []byte(text)})
}

type rebuildTool struct {
	title     string
	kind      string
	status    string
	rawInput  json.RawMessage
	rawOutput json.RawMessage
	content   json.RawMessage
	// 子代理归属。agent 会在部分后续 update 里漏带这些标记，靠下面
	// 「空值不覆盖」的合并语义把首次见到的值留住。
	isSubagent       bool
	subagentOf       string
	subagentThreadID string
	subagentPath     string
}

// RebuildMessages 把线级转录重建成 UI 消息列表。
//
// 规则与旧的落库行为一致：session/prompt 请求产出 user 消息；
// 一轮内的 update 按时序攒进一条时间线，prompt 响应到达时原样落成多条消息
// ——正文被工具调用打断处就是消息的断点。
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
	// 在途（已发出未响应）的 prompt 数：>1 表示 claude promptQueueing 在排队。
	pendingPrompts := 0

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
		// 停止原因挂在本轮最后一段正文上：异常收尾时界面要能就地说明为什么断的。
		lastText := -1
		for i, it := range turn.items {
			if it.kind == model.KindText && trimmed(it.buf) != "" {
				lastText = i
			}
		}
		for i, it := range turn.items {
			switch it.kind {
			case model.KindText, model.KindThought:
				content := trimmed(it.buf)
				if content == "" {
					continue
				}
				msg := model.Message{Role: model.RoleAgent, Kind: it.kind, Content: content}
				if i == lastText && stopReason != "" && stopReason != string(acp.StopEndTurn) {
					msg.Payload = model.JSONMap{"stopReason": stopReason}
				}
				emit(msg, ts)

			case model.KindToolCall:
				tool := turn.tools[it.toolID]
				if tool == nil {
					continue
				}
				// 轮已收尾，没走到终态的工具必然被中止/丢弃——
				// 历史消息不能停留在「执行中」。
				if tool.status == "" || tool.status == "pending" || tool.status == "in_progress" {
					tool.status = "cancelled"
				}
				payload := model.JSONMap{"toolCallId": it.toolID, "status": tool.status}
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
				// 子代理信息只在有值时进 payload，普通工具调用的形状不变。
				if tool.isSubagent {
					payload["isSubagent"] = true
				}
				if tool.subagentOf != "" {
					payload["subagentOf"] = tool.subagentOf
				}
				if tool.subagentThreadID != "" {
					payload["subagentThreadId"] = tool.subagentThreadID
					payload["subagentPath"] = tool.subagentPath
				}
				emit(model.Message{
					Role:    model.RoleAgent,
					Kind:    model.KindToolCall,
					Content: tool.title,
					Payload: payload,
				}, ts)
			}
		}
		turn = nil
	}

	for _, entry := range entries {
		var msg wireMsg
		if err := json.Unmarshal(entry.Msg, &msg); err != nil {
			continue
		}

		switch {
		case entry.Dir == "send" && (msg.Method == "session/new" || msg.Method == "session/load"):
			// 连接重启边界：agent 进程死过一次，后端重连后建/载会话。
			// 上一段若有在途的轮，它的响应永远不会来了——按 cancelled
			// 收尾，内容保留但不再挂起。flush 后 turn 为 nil，接下来
			// session/load 重放的历史 update 会被下面的 turn==nil 跳过，
			// 不会混进任何轮（实时侧的对应机制是 session.replaying）。
			// 旧连接的请求 id 空间同时作废：新连接从小 id 重新计数，
			// 残留的认领表会让旧 id 撞上新请求、误收响应。
			flush(entry.TS, string(acp.StopCancelled))
			pendingPrompts = 0
			sentMethods = map[string]string{}
			agentReqs = map[string]agentReq{}
			if len(msg.ID) > 0 {
				sentMethods[string(msg.ID)] = msg.Method
			}

		case entry.Dir == "send" && msg.Method == "session/prompt":
			if len(msg.ID) > 0 {
				sentMethods[string(msg.ID)] = msg.Method
			}
			var p acp.PromptParams
			if err := json.Unmarshal(msg.Params, &p); err == nil {
				emitUserBlocks(p.Prompt, entry.TS)
			}
			if pendingPrompts > 0 && turn != nil {
				// 上一轮响应未到就来了新 prompt：claude 的 promptQueueing
				// 排队（引导插话）。只产出 user 消息不结轮——当前轮的内容
				// 仍归它自己的响应收尾，排队轮的内容在那之后才开始。
				pendingPrompts++
				continue
			}
			// 上一轮如果没等到响应（进程中途死了），先按无结论落掉。
			flush(entry.TS, "")
			pendingPrompts++
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
			if pendingPrompts > 0 {
				pendingPrompts--
			}
			if pendingPrompts > 0 {
				// 还有排队的 prompt 在途（promptQueueing）：马上开新轮接住
				// 它的内容分片，否则排队轮的回复会因 turn 为空被整段丢弃。
				turn = &rebuildTurn{tools: map[string]*rebuildTool{}}
			}
		}
	}

	return out
}

func applyUpdate(turn *rebuildTurn, u acp.SessionUpdate) {
	switch u.SessionUpdate {
	case acp.UpdateAgentMessageChunk:
		turn.appendChunk(model.KindText, u.Text())

	case acp.UpdateAgentThoughtChunk:
		turn.appendChunk(model.KindThought, u.Text())

	case acp.UpdateToolCall, acp.UpdateToolCallUpdate:
		if u.ToolCallID == "" {
			return
		}
		tool, ok := turn.tools[u.ToolCallID]
		if !ok {
			tool = &rebuildTool{}
			turn.tools[u.ToolCallID] = tool
			// 工具调用首次出现的位置就是它在时间线上的位置，后续 update
			// 只更新内容、不再挪动它。
			turn.items = append(turn.items, &turnItem{
				kind: model.KindToolCall, toolID: u.ToolCallID,
			})
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
		if u.IsSubagentLaunch() {
			tool.isSubagent = true
		}
		if parent := u.SubagentOf(); parent != "" {
			tool.subagentOf = parent
		}
		if threadID, path := u.CodexSubagentThread(); threadID != "" {
			tool.subagentThreadID = threadID
			tool.subagentPath = path
		}
	}
}

func trimmed(b []byte) string {
	return strings.TrimSpace(string(b))
}
