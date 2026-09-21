// Package transfer 负责连接配置的换设备搬家：把服务器（SSH 跳板与观察目标）
// 与数据源（MySQL 连接）两张表导出成 jsonl，再从 jsonl 导回来。
//
// **默认带凭证**（密码、私钥内容与通行短语）：搬家的目的就是到了新机器就能
// 连，少了密码等于没搬。代价是那份文件等同一串明文凭证——所以导出时文件名
// 会标出来（`-secrets`），端点也支持 `?secrets=0` 导一份不带凭证的。
//
// 这不是新开的口子：owner 本来就能经 `/api/servers/{id}/secret` 取回自己存
// 的密码，批量导出只是同一条能力的另一种形态，且同样收在 owner 专属前缀里。
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
	KindKey    = "sshkey"
	KindServer = "server"
	KindSource = "datasource"
)

// maxLineBytes 是单行上限。一条连接配置撑死几百字节，留出两个数量级的余量，
// 超过说明这不是我们导出的文件。
const maxLineBytes = 256 << 10

// KeyRecord 是导出的一把私钥。排在服务器之前：服务器按名字引用它。
type KeyRecord struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
	// PrivateKey / Passphrase 只在带凭证导出时有值。
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	// Fingerprint 即使不带凭证也导出：到了新机器能看出该补哪一把。
	Fingerprint string `json:"fingerprint,omitempty"`
}

// ServerRecord 是导出的一台服务器。
type ServerRecord struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	Auth string `json:"auth"`
	// Key 是私钥库里那把钥匙的**名字**（id 换台机器对不上）。
	Key      string `json:"key,omitempty"`
	KeyPath  string `json:"keyPath,omitempty"`
	Note     string `json:"note,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	// Password / Passphrase 只在带凭证导出时有值。
	Password   string `json:"password,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
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
	// Password 只在带凭证导出时有值。
	Password string `json:"password,omitempty"`
}

// Result 是导入结果：进来了哪些、跳过了哪些。形状与技能库导入一致，
// 前端两处共用同一套提示。
type Result struct {
	Imported []string `json:"imported"`
	Skipped  []Skip   `json:"skipped"`
	// NeedSecret 是导入后还缺凭证的条目。带凭证的包导进来时这里是空的；
	// 用 `?secrets=0` 导的包才会有内容，界面据此提醒去补密码。
	NeedSecret []string `json:"needSecret"`
}

// Skip 是一条没能导入的记录。Reason 是原因码（`exists` / `invalid` /
// `unknown_kind` / `unknown_server` / `no_secret`），用户可见的文案由前端
// 按语言给。
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

// Export 把三张表写成 jsonl。顺序是**私钥 → 服务器 → 数据源**：导入顺序读，
// 后面的靠名字引用前面的，这个次序让引用永远指得到。
//
// secrets 为真时带上密码、私钥内容与通行短语——那正是「搬过去就能用」的
// 前提，也意味着这份文件等同一串明文凭证。
func (s *Service) Export(ctx context.Context, w io.Writer, secrets bool) error {
	enc := json.NewEncoder(w)
	keys, err := s.servers.ListKeys(ctx, "")
	if err != nil {
		return fmt.Errorf("list ssh keys: %w", err)
	}
	keyNameByID := make(map[uint]string, len(keys))
	for _, key := range keys {
		keyNameByID[key.ID] = key.Name
		rec := KeyRecord{
			Kind: KindKey, Name: key.Name, Note: key.Note, Fingerprint: key.Fingerprint,
		}
		if secrets {
			rec.PrivateKey = key.PrivateKey
			rec.Passphrase = key.Passphrase
		}
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("write ssh key %s: %w", key.Name, err)
		}
	}

	servers, err := s.servers.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list servers: %w", err)
	}
	nameByID := make(map[uint]string, len(servers))
	for _, srv := range servers {
		nameByID[srv.ID] = srv.Name
		rec := ServerRecord{
			Kind: KindServer, Name: srv.Name, Host: srv.Host, Port: srv.Port,
			User: srv.User, Auth: srv.Auth, Key: keyNameByID[srv.KeyID],
			KeyPath: srv.KeyPath, Note: srv.Note, Disabled: srv.Disabled,
		}
		if secrets {
			rec.Password = srv.Password
			rec.Passphrase = srv.Passphrase
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
		if secrets {
			rec.Password = ds.Password
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
	keys, err := s.servers.ListKeys(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list ssh keys: %w", err)
	}
	keyIDByName := make(map[string]uint, len(keys))
	for _, key := range keys {
		keyIDByName[key.Name] = key.ID
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
		case KindKey:
			s.importKey(ctx, line, keyIDByName, res)
		case KindServer:
			s.importServer(ctx, line, idByName, keyIDByName, res)
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

// importKey 还原一把私钥。不带私钥内容的记录（`?secrets=0` 导出的）直接
// 跳过——建一把空壳钥匙只会让引用它的服务器连不上，还看不出为什么。
func (s *Service) importKey(ctx context.Context, line []byte, idByName map[string]uint, res *Result) {
	var rec KeyRecord
	if err := json.Unmarshal(line, &rec); err != nil || rec.Name == "" {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "invalid"})
		return
	}
	if _, ok := idByName[rec.Name]; ok {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "exists"})
		return
	}
	if rec.PrivateKey == "" {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "no_secret"})
		res.NeedSecret = append(res.NeedSecret, KindKey+":"+rec.Name)
		return
	}
	key, err := s.servers.CreateKey(ctx, remote.KeyInput{
		Name: rec.Name, PrivateKey: rec.PrivateKey,
		Passphrase: &rec.Passphrase, Note: rec.Note,
	})
	if err != nil {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "invalid"})
		return
	}
	idByName[rec.Name] = key.ID
	res.Imported = append(res.Imported, KindKey+":"+rec.Name)
}

func (s *Service) importServer(ctx context.Context, line []byte, idByName, keyIDByName map[string]uint, res *Result) {
	var rec ServerRecord
	if err := json.Unmarshal(line, &rec); err != nil || rec.Name == "" {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "invalid"})
		return
	}
	if _, ok := idByName[rec.Name]; ok {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "exists"})
		return
	}
	in := remote.Input{
		Name: rec.Name, Host: rec.Host, Port: rec.Port, User: rec.User,
		Auth: rec.Auth, KeyPath: rec.KeyPath, Note: rec.Note,
		Disabled: &rec.Disabled,
	}
	if rec.Password != "" {
		in.Password = &rec.Password
	}
	if rec.Passphrase != "" {
		in.Passphrase = &rec.Passphrase
	}
	if rec.Key != "" {
		// 钥匙没跟过来就照原样建：服务器本身的配置是对的，缺的只是那把钥匙，
		// 在 needSecret 里说清楚比整条跳过有用。
		if id, ok := keyIDByName[rec.Key]; ok {
			in.KeyID = &id
		}
	}
	srv, err := s.servers.Create(ctx, in)
	if err != nil {
		res.Skipped = append(res.Skipped, Skip{Name: rec.Name, Reason: "invalid"})
		return
	}
	idByName[rec.Name] = srv.ID
	res.Imported = append(res.Imported, KindServer+":"+rec.Name)
	if serverNeedsSecret(rec, in.KeyID != nil) {
		res.NeedSecret = append(res.NeedSecret, KindServer+":"+rec.Name)
	}
}

// serverNeedsSecret 判断这台机器还差不差凭证：密码档看密码带没带；公钥档
// 只要接上了库里的钥匙（或走 ssh-agent）就不差。
func serverNeedsSecret(rec ServerRecord, linked bool) bool {
	switch rec.Auth {
	case "key":
		return !linked && rec.KeyPath != ""
	case "both":
		return rec.Password == "" || (!linked && rec.KeyPath != "")
	default:
		return rec.Password == ""
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
	if rec.Password != "" {
		in.Password = &rec.Password
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
	if rec.Password == "" {
		res.NeedSecret = append(res.NeedSecret, KindSource+":"+key)
	}
}

// preview 给坏行一个能认出来的名字：让人知道是文件里的哪一行出了问题。
func preview(line []byte) string {
	const max = 40
	if len(line) > max {
		return string(line[:max]) + "…"
	}
	return string(line)
}
