package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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

// outFile 是一条出站消息里的一个附件。
type outFile struct {
	name string
	data []byte
}

// botRESTFile 发一条带单个附件的消息。
func botRESTFile(ctx context.Context, token, channelID string, payload map[string]any, filename string, data []byte) error {
	return botRESTFiles(ctx, token, channelID, payload, []outFile{{name: filename, data: data}})
}

// botRESTFiles 以 multipart 发一条带若干附件的消息（报告长图 + 原文件、
// send_file 的批量发送都走这里）。attachments 的预登记由这里按 files 顺序
// 补进 payload——调用方手写那段 id/filename 对照表只会写错。
// Discord 单条消息最多 10 个附件、合计 25MB，分批由调用方负责。
func botRESTFiles(ctx context.Context, token, channelID string, payload map[string]any, files []outFile) error {
	if len(files) == 0 {
		return fmt.Errorf("没有要发送的附件")
	}
	atts := make([]map[string]any, 0, len(files))
	for i, f := range files {
		atts = append(atts, map[string]any{"id": i, "filename": f.name})
	}
	payload["attachments"] = atts

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	pj, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := w.WriteField("payload_json", string(pj)); err != nil {
		return err
	}
	for i, f := range files {
		part, err := w.CreateFormFile(fmt.Sprintf("files[%d]", i), f.name)
		if err != nil {
			return err
		}
		if _, err := part.Write(f.data); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://discord.com/api/v10/channels/"+channelID+"/messages", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", "acpp (https://github.com/acpp, 0.1)")
	// 附件几 MB 起步，超时给宽些。
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("上传附件 → %s: %s", resp.Status, raw)
	}
	return nil
}
