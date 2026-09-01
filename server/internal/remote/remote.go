// Package remote 负责远程服务器：连接配置（SSH）、命令执行外壳，以及
// 挂给会话的只读观察工具面（文件 / Docker / 主机信息）。
//
// 三条设计主线（adr-019）：
//   - **代码是事实源**。工具里不含任何具体项目的知识——远程路径、容器名、
//     日志位置全靠 AI 从项目代码（compose / Makefile / CI）推断，工具只提供
//     通用原语去验证那些推断。
//   - **不做项目隔离**。一台机器上跑多个项目是常态，按项目切会把 AI 需要的
//     上下文一起切掉。配置一台服务器，就等于授权 AI 观察整台机器。
//   - **全部只读**。不提供裸命令通道：给了它，其余工具的护栏与输出预算就
//     全废了。AI 真要跑任意命令时有自带 shell，那是用户看得见的显式选择。
//
// 服务器同时是数据源的拨号跳板（datasource 经 Servers 接口取配置）。
package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"gorm.io/gorm"

	"acpp/server/internal/mcp"
	"acpp/server/internal/model"
	"acpp/server/internal/service"
	"acpp/server/internal/sshdial"
)

// Sessions 是会话侧的最小依赖：把 MCP 端点的 token 换回会话身份，以及为
// 要挂载工具面的会话备好 token。用接口而不是直接 import 会话服务，理由与
// datasource 一致——两个业务包因此不互相 import。
type Sessions interface {
	SessionByMCPToken(ctx context.Context, token string) (uint, string, error)
	EnsureMCPToken(ctx context.Context, sessionID uint) (string, error)
}

// Calls 是调用观测的最小依赖。记录是尽力而为的旁路，所以没有返回值：
// 观测失败不该让 AI 的工具调用跟着失败。
type Calls interface {
	Record(ctx context.Context, rec model.MCPCall)
}

// Service 是服务器的业务面。
type Service struct {
	db       *gorm.DB
	sessions Sessions
	calls    Calls
	// mcpBase 是 agent 回连的 MCP 端点前缀。
	mcpBase string
	// peerTok 是非会话调用方（discord 子区）的回连凭证。
	peerTok mcp.PeerTokens
}

func NewService(db *gorm.DB, sessions Sessions, addr string) *Service {
	return &Service{db: db, sessions: sessions, mcpBase: mcpBaseURL(addr)}
}

// WithCalls 挂上调用观测。分开一个 setter 而不是塞进 NewService：
// 记录是可选旁路，缺了服务器功能照常跑。
func (s *Service) WithCalls(calls Calls) *Service {
	s.calls = calls
	return s
}

// mcpBaseURL 从监听地址推导 MCP 前缀：agent 子进程与我们同机，
// 监听 0.0.0.0 时也走回环回连。
func mcpBaseURL(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "48080"
	}
	return "http://127.0.0.1:" + port + "/api/mcp/server/"
}

// Input 是新建/更新的入参。凭证类字段用指针表示「没传就不改」——
// 密码留空要能表达「保持原样」，而不是「清空」。
type Input struct {
	Name       string  `json:"name"`
	Host       string  `json:"host"`
	Port       int     `json:"port"`
	User       string  `json:"user"`
	Auth       string  `json:"auth"`
	Password   *string `json:"password"`
	KeyPath    string  `json:"keyPath"`
	Passphrase *string `json:"passphrase"`
	Note       string  `json:"note"`
	Disabled   *bool   `json:"disabled"`
}

// blank 报告这份入参有没有实际内容。凭证类字段不算——只带一个新密码
// 来测连接是合理用法，那时其余字段本来就该沿用已存记录。
func (in Input) blank() bool {
	return strings.TrimSpace(in.Name) == "" &&
		strings.TrimSpace(in.Host) == "" &&
		strings.TrimSpace(in.User) == "" &&
		strings.TrimSpace(in.Auth) == "" &&
		strings.TrimSpace(in.KeyPath) == "" &&
		in.Port == 0
}

// List 按名字排序返回全部服务器（配置页用）。
func (s *Service) List(ctx context.Context) ([]model.Server, error) {
	var out []model.Server
	if err := s.db.WithContext(ctx).Order("name").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	for i := range out {
		decorate(&out[i])
	}
	return out, nil
}

// Enabled 返回启用中的服务器——挂给 AI 的工具面用。
func (s *Service) Enabled(ctx context.Context) ([]model.Server, error) {
	var out []model.Server
	err := s.db.WithContext(ctx).Where("disabled = ?", false).Order("name").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("list enabled servers: %w", err)
	}
	for i := range out {
		decorate(&out[i])
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id uint) (*model.Server, error) {
	var srv model.Server
	if err := s.db.WithContext(ctx).First(&srv, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, service.ErrNotFound
		}
		return nil, fmt.Errorf("get server: %w", err)
	}
	decorate(&srv)
	return &srv, nil
}

// ServerByID 实现 datasource.Servers：数据源开着隧道时靠它拿跳板机配置。
// 与 Get 的差别只在语义——这条路上的调用方不是人而是拨号逻辑。
func (s *Service) ServerByID(ctx context.Context, id uint) (*model.Server, error) {
	return s.Get(ctx, id)
}

// ByName 按名字取一台机器（AI 调工具时填的就是名字）。
func (s *Service) ByName(ctx context.Context, name string) (*model.Server, error) {
	var srv model.Server
	err := s.db.WithContext(ctx).Where("name = ?", strings.TrimSpace(name)).First(&srv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, service.ErrNotFound
		}
		return nil, fmt.Errorf("get server by name: %w", err)
	}
	decorate(&srv)
	return &srv, nil
}

func (s *Service) Create(ctx context.Context, in Input) (*model.Server, error) {
	srv := model.Server{Port: 22, Auth: sshdial.AuthPassword}
	if err := apply(&srv, in); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(&srv).Error; err != nil {
		return nil, wrapWrite(err, srv)
	}
	decorate(&srv)
	return &srv, nil
}

func (s *Service) Update(ctx context.Context, id uint, in Input) (*model.Server, error) {
	srv, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := apply(srv, in); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Save(srv).Error; err != nil {
		return nil, wrapWrite(err, *srv)
	}
	decorate(srv)
	return srv, nil
}

// Delete 删掉一台服务器。被数据源引用着的不让删——那会让那条数据源的
// 隧道无声地拨不出去，而错误要等到下次查询才浮现。
func (s *Service) Delete(ctx context.Context, id uint) error {
	var used int64
	err := s.db.WithContext(ctx).Model(&model.DataSource{}).
		Where("server_id = ?", id).Count(&used).Error
	if err != nil {
		return fmt.Errorf("count datasource refs: %w", err)
	}
	if used > 0 {
		return fmt.Errorf("%w: 还有 %d 条数据源把它当跳板机，先去数据库页改掉再删", service.ErrInvalid, used)
	}
	res := s.db.WithContext(ctx).Delete(&model.Server{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete server: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return service.ErrNotFound
	}
	return nil
}

// Test 拨一次 SSH 确认配置可用，返回对端的版本横幅（SSH-2.0-OpenSSH_… 那行）。
// id 为 0 时测的是入参本身（新建时还没落库）；带 id 则以已存记录为底，
// 让留空的密码沿用原值。
func (s *Service) Test(ctx context.Context, id uint, in Input) (string, error) {
	probe := model.Server{Port: 22, Auth: sshdial.AuthPassword}
	if id > 0 {
		existing, err := s.Get(ctx, id)
		if err != nil {
			return "", err
		}
		probe = *existing
	}
	// 入参是空的表示「就测这条已存记录」——列表页那个测试按钮不带表单内容。
	// 不加这个判断的话，apply 会拿一身空字段把已存记录覆盖掉，然后报
	// 「名称不能为空」：明明什么都没改，测试却失败了。
	if id == 0 || !in.blank() {
		if err := apply(&probe, in); err != nil {
			return "", err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, sshdial.DefaultTimeout)
	defer cancel()
	client, err := sshdial.Dial(ctx, configOf(&probe))
	if err != nil {
		return "", wrapDial(err)
	}
	defer client.Close()
	return string(client.ServerVersion()), nil
}

// configOf 把一条服务器记录翻译成拨号参数。
func configOf(srv *model.Server) sshdial.Config {
	return sshdial.Config{
		Host:       srv.Host,
		Port:       srv.Port,
		User:       srv.User,
		Auth:       srv.Auth,
		Password:   srv.Password,
		KeyPath:    srv.KeyPath,
		Passphrase: srv.Passphrase,
	}
}

// wrapDial 把 sshdial 的配置类错误换成本项目的哨兵错误，
// httpapi 才会映射成 400 而不是 500。
func wrapDial(err error) error {
	if errors.Is(err, sshdial.ErrInvalid) {
		return fmt.Errorf("%w: %s", service.ErrInvalid,
			strings.TrimPrefix(err.Error(), sshdial.ErrInvalid.Error()+": "))
	}
	return err
}

func apply(srv *model.Server, in Input) error {
	srv.Name = strings.TrimSpace(in.Name)
	srv.Host = strings.TrimSpace(in.Host)
	srv.User = strings.TrimSpace(in.User)
	srv.KeyPath = strings.TrimSpace(in.KeyPath)
	srv.Note = strings.TrimSpace(in.Note)
	if a := strings.TrimSpace(in.Auth); a != "" {
		srv.Auth = a
	}
	if in.Port > 0 {
		srv.Port = in.Port
	}
	// 凭证类字段：**空串也视为「不修改」**，不是「清空」。编辑时后端不下发
	// 密码（响应里只有 hasPassword），表单里那一格就是空的，提交时原样发回
	// 一个空串——按字面处理的话，用户改一次备注就把生产机的密码清了。
	// datasource 那边踩过这个坑，这里照同样的口径。
	if in.Password != nil && *in.Password != "" {
		srv.Password = *in.Password
	}
	if in.Passphrase != nil && *in.Passphrase != "" {
		srv.Passphrase = *in.Passphrase
	}
	if in.Disabled != nil {
		srv.Disabled = *in.Disabled
	}
	return validate(srv)
}

func validate(srv *model.Server) error {
	switch {
	case srv.Name == "":
		return fmt.Errorf("%w: 名称不能为空", service.ErrInvalid)
	case strings.ContainsAny(srv.Name, " \t/\\"):
		// 名字是 AI 调工具时填的标识，带空格与斜杠会让它难以准确复述。
		return fmt.Errorf("%w: 名称不能含空格或斜杠", service.ErrInvalid)
	case srv.Host == "":
		return fmt.Errorf("%w: 主机不能为空", service.ErrInvalid)
	case srv.User == "":
		return fmt.Errorf("%w: 用户名不能为空", service.ErrInvalid)
	case !sshdial.ValidAuth(srv.Auth):
		return fmt.Errorf("%w: 未知的验证方式 %q", service.ErrInvalid, srv.Auth)
	}
	return nil
}

func decorate(srv *model.Server) {
	srv.HasPassword = srv.Password != ""
	srv.HasPassphrase = srv.Passphrase != ""
}

func wrapWrite(err error, srv model.Server) error {
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return fmt.Errorf("%w: 名称 %s 已存在", service.ErrInvalid, srv.Name)
	}
	return fmt.Errorf("save server: %w", err)
}
