package discord

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acpp/server/internal/acp"
)

// Discord 消息附件 → prompt 内容块。
//
// 附件一律**先落盘**到 <workdir>/.acpp-uploads/：光把内容嵌进 prompt，
// agent 就只能「看」不能「用」——要移动、改名、当素材编进代码的场景全断。
// 落盘后按类型追加内容块：图片双发 image block（模型直接看图），小文本
// 内嵌全文（与网页会话 32KB 的 resourceLinkThreshold 同口径），其余给
// resource_link 让 agent 按需自行读。目录写进 .git/info/exclude，不脏仓库。

// attachment 是 MESSAGE_CREATE 里的一个附件。
type attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
}

const (
	uploadsDir = ".acpp-uploads"
	// attachMaxSize 是下载上限：Discord 免费档单附件本来就 25MB 封顶，
	// 超限（或下载失败）只在文本里说明，不拦回合。
	attachMaxSize = 25 << 20
	// attachEmbedLimit 与网页会话 @ 文件的内嵌阈值同口径。
	attachEmbedLimit = 32 * 1024
	// attachImageLimit 是发 image block 的上限——base64 后要整块进 prompt。
	attachImageLimit = 5 << 20
)

// attachmentBlocks 把一批附件转成内容块，返回（blocks, 给用户看的问题说明）。
// 单个附件失败不拦别的，也不拦回合——说明行会拼进提示消息。
func (s *Service) attachmentBlocks(ctx context.Context, workdir string, atts []attachment) ([]acp.ContentBlock, []string) {
	var blocks []acp.ContentBlock
	var notes []string
	for _, att := range atts {
		b, err := s.attachmentBlock(ctx, workdir, att)
		if err != nil {
			notes = append(notes, fmt.Sprintf("⚠️ 附件 %s 没接住：%s", att.Filename, trimRunes(err.Error(), 200)))
			continue
		}
		blocks = append(blocks, b...)
	}
	return blocks, notes
}

func (s *Service) attachmentBlock(ctx context.Context, workdir string, att attachment) ([]acp.ContentBlock, error) {
	if att.Size > attachMaxSize {
		return nil, fmt.Errorf("太大（%d MB，上限 25MB）", att.Size>>20)
	}
	data, err := downloadAttachment(ctx, att.URL)
	if err != nil {
		return nil, err
	}
	path, err := saveUpload(workdir, att, data)
	if err != nil {
		return nil, err
	}

	uri := "file://" + path
	switch {
	case strings.HasPrefix(att.ContentType, "image/") && len(data) <= attachImageLimit:
		return []acp.ContentBlock{
			acp.ImageBlock(base64.StdEncoding.EncodeToString(data), att.ContentType),
			acp.ResourceLinkBlock(uri, att.Filename, int64(len(data))),
		}, nil
	case isTextAttachment(att.ContentType, data) && len(data) <= attachEmbedLimit:
		return []acp.ContentBlock{acp.ResourceBlock(uri, string(data))}, nil
	default:
		return []acp.ContentBlock{acp.ResourceLinkBlock(uri, att.Filename, int64(len(data)))}, nil
	}
}

// isTextAttachment 判断内容是不是文本：content_type 说了算，没说就看
// 开头有没有 NUL（二进制最省事的指纹）。
func isTextAttachment(contentType string, data []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.HasPrefix(ct, "text/") ||
		strings.HasPrefix(ct, "application/json") ||
		strings.HasPrefix(ct, "application/xml") ||
		strings.HasPrefix(ct, "application/x-yaml") {
		return true
	}
	if ct != "" && ct != "application/octet-stream" {
		return false
	}
	probe := data
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	for _, c := range probe {
		if c == 0 {
			return false
		}
	}
	return len(probe) > 0
}

// saveUpload 把附件写进 <workdir>/.acpp-uploads/<附件id>-<原名>：id 前缀
// 防重名互踩，原名保留给 agent 认。目录首次创建时登进 .git/info/exclude。
func saveUpload(workdir string, att attachment, data []byte) (string, error) {
	dir := filepath.Join(workdir, uploadsDir)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("建上传目录: %w", err)
		}
		excludeUploads(workdir)
	}
	name := att.ID + "-" + filepath.Base(att.Filename)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写附件: %w", err)
	}
	return path, nil
}

// excludeUploads 把上传目录写进 .git/info/exclude（不碰工作树里的
// .gitignore——那是用户仓库的文件）。失败只是仓库会显脏，不值得报错。
func excludeUploads(workdir string) {
	path := filepath.Join(workdir, ".git", "info", "exclude")
	existing, err := os.ReadFile(path)
	line := uploadsDir + "/"
	if err == nil && strings.Contains(string(existing), line) {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n%s\n", line)
}

func downloadAttachment(ctx context.Context, url string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, attachMaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("下载: %w", err)
	}
	if len(data) > attachMaxSize {
		return nil, fmt.Errorf("太大（上限 25MB）")
	}
	return data, nil
}
