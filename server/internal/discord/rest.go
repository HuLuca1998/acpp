package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// botREST 以 bot 身份调一次 Discord REST。out 为 nil 时丢弃响应体。
// 调用量极低（配置面 + 每频道一次的 /init），不做限速队列；撞上 429 让
// 错误浮出来按失败处理。
func botREST(ctx context.Context, token, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://discord.com/api/v10"+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "acpp (https://github.com/acpp, 0.1)")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s → %s: %s", method, path, resp.Status, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// interactionCallback 回一次 interaction。种类：4=新消息、5=deferred、
// 7=原地改卡、9=弹 modal。3 秒时限，不重试——迟到的 callback 平台直接丢弃。
func interactionCallback(token, interactionID, interactionToken string, kind int, data map[string]any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	body := map[string]any{"type": kind}
	if data != nil {
		body["data"] = data
	}
	return botREST(ctx, token, "POST",
		fmt.Sprintf("/interactions/%s/%s/callback", interactionID, interactionToken), body, nil)
}

// noMentions 是所有出站消息统一带的 allowed_mentions 收紧——平台默认是
// parse-all，编辑时漏带还会重新解析（实测），所以收在这一个出口。
func noMentions() map[string]any {
	return map[string]any{"parse": []string{}}
}
