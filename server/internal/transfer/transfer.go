// Package transfer 负责连接配置的换设备搬家：把服务器（SSH 跳板与观察目标）
// 与数据源（MySQL 连接）两张表导出成 jsonl，再从 jsonl 导回来。
//
// **不含凭证**。密码、私钥口令在这个项目里永不出 API（model 上就打着
// `json:"-"`），搬家也不破这条规矩：导出的是「连到哪儿、用什么账号、走不走
// 跳板」，到了新机器由人把密码补上。私钥本身也不搬——KeyPath 记的是路径，
// 文件权限跟着文件系统走。
//
// 选 jsonl 不选 json 数组：一行一条记录，坏掉一行不影响其余，人也能直接用
// grep 看里面有什么。
package transfer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"acpp/server/internal/datasource"
	"acpp/server/internal/model"
	"acpp/server/internal/remote"
	"acpp/server/internal/service"
)

// 记录种类。每行开头的 kind 决定其余字段怎么读。
const (
	KindServer = "server"
	KindSource = "datasource"
)

// maxLineBytes 是单行上限。一条连接配置撑死几百字节，留出两个数量级的余量，
// 超过说明这不是我们导出的文件。
const maxLineBytes = 256 << 10

// ServerRecord 是导出的一台服务器。
type ServerRecord struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Auth     string `json:"auth"`
	KeyPath  string `json:"keyPath,omitempty"`
	Note     string `json:"note,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

// SourceRecord 是导出的一条数据源。
//
// Server 是跳板机的**名字**而不是 id：id 是本机自增的，换台机器必然对不上。
type SourceRecord struct {
	Kind       string `json:"kind"`
	Project    string `json:"project"`
	Env        string `json:"env"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Database   string `json:"database"`
	Params     string `json:"params,omitempty"`
	Note       string `json:"note,omitempty"`
	ReadOnly   bool   `json:"readOnly"`
	Disabled   bool   `json:"disabled,omitempty"`
	SSHEnabled bool   `json:"sshEnabled,omitempty"`
	Server     string `json:"server,omitempty"`
}

// Result 是导入结果：进来了哪些、跳过了哪些。形状与技能库导入一致，
// 前端两处共用同一套提示。
type Result struct {
	Imported []string `json:"imported"`
	Skipped  []Skip   `json:"skipped"`
	// NeedSecret 是导入后还缺凭证的条目——导出不带密码，这些连接在补上
	// 之前连不通。界面据此提醒，省得人以为搬完就能用。
	NeedSecret []string `json:"needSecret"`
}

// Skip 是一条没能导入的记录。Reason 是原因码（`exists` / `invalid` /
// `unknown_kind` / `unknown_server`），用户可见的文案由前端按语言给。
type Skip struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Service 编排两张表的导出与导入。
type Service struct {
	servers *remote.Service
	sources *datasource.Service
}

func New(servers *remote.Service, sources *datasource.Service) *Service {
	return &Service{servers: servers, sources: sources}
}

// Export 把两张表写成 jsonl。**服务器在前、数据源在后**：导入是顺序读的，
// 这样数据源引用的跳板机已经先建好了。
func (s *Service) Export(ctx context.Context, w io.Writer) error {
	servers, err := s.servers.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list servers: %w", err)
	}
	enc := json.NewEncoder(w)
	nameByID := make(map[uint]string, len(servers))
	for _, srv := range servers {
		nameByID[srv.ID] = srv.Name
		rec := ServerRecord{
			Kind: KindServer, Name: srv.Name, Host: srv.Host, Port: srv.Port,
			User: srv.User, Auth: srv.Auth, KeyPath: srv.KeyPath,
			Note: srv.Note, Disabled: srv.Disabled,
		}
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("write server %s: %w", srv.Name, err)
		}
	}

	sources, err := s.allSources(ctx)
	if err != nil {
		return err
	}
	for _, ds := range sources {
		rec := SourceRecord{
			Kind: KindSource, Project: ds.Project, Env: ds.Env, Host: ds.Host,
			Port: ds.Port, User: ds.User, Database: ds.Database, Params: ds.Params,
			Note: ds.Note, ReadOnly: ds.ReadOnly, Disabled: ds.Disabled,
			SSHEnabled: ds.SSHEnabled, Server: nameByID[ds.ServerID],
		}
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("write data source %s/%s: %w", ds.Project, ds.Env, err)
		}
	}
	return nil
}

// allSources 翻页取全部数据源。列表端点是分页的（那是给页面用的），
// 搬家要的是整张表。
func (s *Service) allSources(ctx context.Context) ([]model.DataSource, error) {
	const pageSize = 200
	var out []model.DataSource
	for page := 1; ; page++ {
		batch, total, err := s.sources.List(ctx, datasource.ListFilter{}, page, pageSize, "")
		if err != nil {
			return nil, fmt.Errorf("list data sources: %w", err)
		}
		out = append(out, batch...)
		if len(batch) == 0 || int64(len(out)) >= total {
			return out, nil
		}
	}
}

// Import 从 jsonl 还原连接配置。已存在的同名条目跳过而不是覆盖——那是这台
// 机器上用户自己配的，搬家不该无声地把它换掉。坏掉的行只跳过自己，不牵连
// 整份文件：一行一条正是 jsonl 的用处。
func (s *Service) Import(ctx context.Context, r io.Reader) (*Result, error) {
	known, err := s.servers.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	idByName := make(map[string]uint, len(known))
	for _, srv := range known {
		idByName[srv.Name] = srv.ID
	}
	sources, err := s.allSources(ctx)
	if err != nil {
		return nil, err
	}
	seenSource := make(map[string]bool, len(sources))
	for _, ds := range sources {
		seenSource[ds.Project+"/"+ds.Env] = true
	}

	res := &Result{Imported: []string{}, Skipped: []Skip{}, NeedSecret: []string{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var head struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			res.Skipped = append(res.Skipped, Skip{Name: preview(line), Reason: "invalid"})
			continue
		}
		switch head.Kind {
		case KindServer:
			s.importServer(ctx, line, idByName, res)
		case KindSource:
			s.importSource(ctx, line, idByName, seenSource, res)
		default:
			res.Skipped = append(res.Skipped, Skip{Name: head.Name, Reason: "unknown_kind"})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: read archive: %s", service.ErrInvalid, err)
	}
	return res, nil
}

func (s *Service) importServer(ctx context.Context, line []byte, idByName map[string]uint, res *Result) {
	var rec ServerRecord
	if err := json.Unmarshal(line, &rec); err != nil || rec.Name == "" {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "invalid"})
		return
	}
	if _, ok := idByName[rec.Name]; ok {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "exists"})
		return
	}
	srv, err := s.servers.Create(ctx, remote.Input{
		Name: rec.Name, Host: rec.Host, Port: rec.Port, User: rec.User,
		Auth: rec.Auth, KeyPath: rec.KeyPath, Note: rec.Note,
		Disabled: &rec.Disabled,
	})
	if err != nil {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "invalid"})
		return
	}
	idByName[rec.Name] = srv.ID
	res.Imported = append(res.Imported, KindServer+":"+rec.Name)
	// password / key 档都要凭证；key 档留空是走 ssh-agent，那种不用补。
	if rec.Auth != "key" || rec.KeyPath != "" {
		res.NeedSecret = append(res.NeedSecret, KindServer+":"+rec.Name)
	}
}

func (s *Service) importSource(ctx context.Context, line []byte, idByName map[string]uint, seen map[string]bool, res *Result) {
	var rec SourceRecord
	if err := json.Unmarshal(line, &rec); err != nil || rec.Project == "" || rec.Env == "" {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Project + "/" + rec.Env, Reason: "invalid"})
		return
	}
	key := rec.Project + "/" + rec.Env
	if seen[key] {
		res.Skipped = append(res.Skipped, Skip{Name: key, Reason: "exists"})
		return
	}
	in := datasource.Input{
		Project: rec.Project, Env: rec.Env, Host: rec.Host, Port: rec.Port,
		User: rec.User, Database: rec.Database, Params: rec.Params, Note: rec.Note,
		ReadOnly: &rec.ReadOnly, Disabled: &rec.Disabled, SSHEnabled: &rec.SSHEnabled,
	}
	if rec.SSHEnabled {
		id, ok := idByName[rec.Server]
		if !ok {
			// 跳板机不在这台机器上，也不在这份文件里。硬建出来只会得到一条
			// 连不通的数据源，不如说清楚缺了谁。
			res.Skipped = append(res.Skipped, Skip{Name: key, Reason: "unknown_server"})
			return
		}
		in.ServerID = &id
	}
	if _, err := s.sources.Create(ctx, in); err != nil {
		res.Skipped = append(res.Skipped, Skip{Name: key, Reason: "invalid"})
		return
	}
	seen[key] = true
	res.Imported = append(res.Imported, KindSource+":"+key)
	res.NeedSecret = append(res.NeedSecret, KindSource+":"+key)
}

// preview 给坏行一个能认出来的名字：让人知道是文件里的哪一行出了问题。
func preview(line []byte) string {
	const max = 40
	if len(line) > max {
		return string(line[:max]) + "…"
	}
	return string(line)
}
