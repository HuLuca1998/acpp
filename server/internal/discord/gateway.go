package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/coder/websocket"
)

// GUILDS 送 GUILD_CREATE/DELETE（状态面 + 命令注册时机）；
// GUILD_MESSAGES + MESSAGE_CONTENT 是子区对话的输入通道（MESSAGE_CONTENT
// 是特权 intent，开发者门户已开）。
const gatewayIntents = (1 << 0) | (1 << 9) | (1 << 15)

// 重连退避与 SSE 消费同一姿势：翻倍、封顶、连上重置。
const (
	backoffMin = 2 * time.Second
	backoffMax = time.Minute
)

// gwPayload 是 Gateway 双向帧的通用外壳。
type gwPayload struct {
	Op int             `json:"op"`
	T  string          `json:"t,omitempty"`
	S  *int64          `json:"s,omitempty"`
	D  json.RawMessage `json:"d,omitempty"`
}

// gatewayOnce 建立一次 Gateway 长连接：HELLO/IDENTIFY/心跳，把 dispatch
// 事件交给 handle（事件名 + 原始载荷）。断开原因作为错误返回，重连循环
// 归 Service 管。不做 resume——私人 bot 的会话额度（1000/天）远用不完，
// 全新 IDENTIFY 简单且足够。
func gatewayOnce(ctx context.Context, token string, onReady func(), handle func(t string, d json.RawMessage)) error {
	// 网关地址按文档每次现取（它可能迁移），取不到再用兜底常量。
	gwURL := "wss://gateway.discord.gg"
	var info struct {
		URL string `json:"url"`
	}
	if err := botREST(ctx, token, "GET", "/gateway/bot", nil, &info); err == nil && info.URL != "" {
		gwURL = info.URL
	}

	conn, _, err := websocket.Dial(ctx, gwURL+"/?v=10&encoding=json", nil)
	if err != nil {
		return fmt.Errorf("连 Gateway: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(1 << 22) // GUILD_CREATE 可以很大

	read := func() (gwPayload, error) {
		var p gwPayload
		_, data, err := conn.Read(ctx)
		if err != nil {
			return p, err
		}
		return p, json.Unmarshal(data, &p)
	}
	write := func(p any) error {
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, data)
	}

	hello, err := read()
	if err != nil || hello.Op != 10 {
		return fmt.Errorf("等 HELLO: op=%d err=%w", hello.Op, err)
	}
	var hb struct {
		HeartbeatInterval int `json:"heartbeat_interval"`
	}
	if err := json.Unmarshal(hello.D, &hb); err != nil {
		return fmt.Errorf("解 HELLO: %w", err)
	}

	if err := write(map[string]any{"op": 2, "d": map[string]any{
		"token":   token,
		"intents": gatewayIntents,
		"properties": map[string]string{
			"os": "macos", "browser": "acpp", "device": "acpp",
		},
	}}); err != nil {
		return fmt.Errorf("IDENTIFY: %w", err)
	}

	// 心跳带最近的序号；读写并发，写错误经 channel 收敛到主循环。
	var seq int64
	hbCtx, stopHB := context.WithCancel(ctx)
	defer stopHB()
	hbErr := make(chan error, 1)
	go func() {
		tick := time.NewTicker(time.Duration(hb.HeartbeatInterval) * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-tick.C:
				// 心跳的序号放 d 字段（op:1 的载荷就是序号本身）——放进
				// s 字段平台回 4002 Error while decoding payload（实测踩过）。
				if err := write(map[string]any{"op": 1, "d": seq}); err != nil {
					hbErr <- err
					return
				}
			}
		}
	}()

	for {
		select {
		case err := <-hbErr:
			return fmt.Errorf("心跳: %w", err)
		default:
		}
		p, err := read()
		if err != nil {
			return err
		}
		if p.S != nil {
			seq = *p.S
		}
		switch p.Op {
		case 0:
			if p.T == "READY" {
				onReady()
			}
			handle(p.T, p.D)
		case 7, 9:
			// 平台要求重连/会话失效：退出让上层重来。
			return fmt.Errorf("Gateway 要求重连 (op=%d)", p.Op)
		}
	}
}
