package upload

import (
	"acpp/server/internal/service"

	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 上传的本地文件落在各自身份的家目录下（owner 是工作区根，租户是自己的
// root），因此隔离是**目录本身**给的，不需要再加一层过滤：租户连别人的
// 目录都进不去（adr-007 的路径闸）。
//
// 为什么落盘而不是把内容直接塞进消息：@ 引用这条路已经通了（后端读文件、
// 以 resource 块嵌进 prompt），落盘之后上传的文件与工作区里的文件就是同
// 一种东西，一套代码。而且「历史上传」这件事本身要求文件留着。
const uploadDirName = ".acpp-uploads"

// UploadedFile 是一个已落盘的上传件。
type UploadedFile struct {
	// Name 是用户那边的原始文件名。
	Name string `json:"name"`
	// Path 是绝对路径，前端拿它当普通的 @ 文件引用用。
	Path string `json:"path"`
	Size int64  `json:"size"`
	// Hash 是内容的 sha256（十六进制全串）。
	Hash string `json:"hash"`
	// Reused 为 true 表示这次上传命中了已有的同内容文件，没有重新写盘。
	Reused     bool      `json:"reused"`
	UploadedAt time.Time `json:"uploadedAt"`
}

// UploadDir 是该身份存放上传件的目录（不存在则建）。
func UploadDir(s service.Scope) (string, error) {
	dir := filepath.Join(service.Home(s), uploadDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("upload dir: %w", err)
	}
	return dir, nil
}

// SaveUpload 落盘一个上传件，按内容 hash 去重。
//
// 路径是 `<上传目录>/<hash 前 12 位>/<原名>`：hash 做目录名保证同内容落
// 在同一处，于是「已经传过就不再写一遍」只是一次 Stat；**原名留在最后
// 一段**，这样路径末段就是用户认识的文件名——附件芯片、prompt 里的
// resource uri、agent 自己去读，看到的都是 `report.csv` 而不是一串哈希。
//
// 同内容不同原名会在同一个 hash 目录下存两份——那是刻意的：文件名本身
// 是信息，用户把 `2024-report.csv` 和 `2025-report.csv` 传成同样的内容，
// 多半是他弄错了而不是想复用，留两份至少能看出来。
func SaveUpload(s service.Scope, name string, src io.Reader) (*UploadedFile, error) {
	name = sanitizeUploadName(name)
	if name == "" {
		return nil, fmt.Errorf("%w: file name is required", service.ErrInvalid)
	}
	dir, err := UploadDir(s)
	if err != nil {
		return nil, err
	}

	// 先落到临时文件再改名：一边读一边算 hash，避免把整个文件读进内存
	// （上传的可能是几百 MB 的数据导出）。
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("upload temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	sum := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, sum), src)
	closeErr := tmp.Close()
	if err != nil {
		return nil, fmt.Errorf("upload write: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("upload write: %w", closeErr)
	}

	hash := hex.EncodeToString(sum.Sum(nil))
	bucket := filepath.Join(dir, hash[:12])
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		return nil, fmt.Errorf("upload bucket: %w", err)
	}
	target := filepath.Join(bucket, name)

	if info, statErr := os.Stat(target); statErr == nil {
		// 同内容同名已经在了：直接用它，不重写盘也不改时间戳。
		return &UploadedFile{
			Name:       name,
			Path:       target,
			Size:       info.Size(),
			Hash:       hash,
			Reused:     true,
			UploadedAt: info.ModTime(),
		}, nil
	}

	if err := os.Rename(tmpPath, target); err != nil {
		return nil, fmt.Errorf("upload save: %w", err)
	}
	return &UploadedFile{
		Name:       name,
		Path:       target,
		Size:       size,
		Hash:       hash,
		UploadedAt: time.Now(),
	}, nil
}

// ListUploads 列出该身份上传过的文件，最近的在前。
func ListUploads(s service.Scope) ([]UploadedFile, error) {
	dir, err := UploadDir(s)
	if err != nil {
		return nil, err
	}
	buckets, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list uploads: %w", err)
	}

	out := []UploadedFile{}
	for _, b := range buckets {
		// 只认 `<12 位 hash>/` 这样的桶，别的东西不是本功能写进去的。
		if !b.IsDir() || len(b.Name()) != 12 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, b.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			out = append(out, UploadedFile{
				Name:       f.Name(),
				Path:       filepath.Join(dir, b.Name(), f.Name()),
				Size:       info.Size(),
				Hash:       b.Name(),
				UploadedAt: info.ModTime(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UploadedAt.After(out[j].UploadedAt)
	})
	return out, nil
}

// DeleteUpload 删掉一个上传件。只收 hash 与文件名，不收路径——收路径就
// 得再防一次 `../`，而这里根本不需要那个能力。
func DeleteUpload(s service.Scope, hash, name string) error {
	dir, err := UploadDir(s)
	if err != nil {
		return err
	}
	name = sanitizeUploadName(name)
	if len(hash) < 12 || name == "" {
		return fmt.Errorf("%w: hash and name are required", service.ErrInvalid)
	}
	bucket := filepath.Join(dir, hash[:12])
	if err := os.Remove(filepath.Join(bucket, name)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: upload %s", service.ErrNotFound, name)
		}
		return fmt.Errorf("delete upload: %w", err)
	}
	// 桶里最后一个也删了就把桶收掉，不留一地空目录。非空时 Remove 会
	// 自己失败，正是我们想要的，不必先判断。
	_ = os.Remove(bucket)
	return nil
}

// sanitizeUploadName 把上传方给的文件名压成一个安全的**纯文件名**。
//
// 浏览器给的名字是不可信输入：`../../etc/passwd`、带斜杠的路径、纯
// `..`，都得在拼进路径之前处理掉。同时压掉前导点，免得传上来的文件在
// 列表里被当成隐藏文件跳过。
func sanitizeUploadName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.TrimLeft(name, ".")
	if name == "" || name == "/" {
		return ""
	}
	// 连字符是我们的 `<hash>-<名字>` 分隔符，名字里的连字符不影响切分
	// （Cut 只切第一个），但路径分隔符必须没有。
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	if len(name) > 120 {
		name = name[len(name)-120:]
	}
	return name
}
