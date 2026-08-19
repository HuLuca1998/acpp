// Package datasource 负责外部 MySQL 数据源：连接配置、SSH 隧道拨号、
// 库表探查与多段语句执行，以及把这些能力做成 MCP 工具挂给会话。
//
// 三条设计主线：
//   - **连接是一次性的**。每次调用现拨现关（含 SSH 隧道），不留连接池——
//     面板的调用频率不值得为保活、重连、并发争用付复杂度。
//   - **项目是可见性边界**。会话只能看见自己所在项目的数据源（见 scope.go），
//     执行点在 ForCwd，MCP 与斜杠命令共用。
//   - **权限交给数据库**。软件层不做 SQL 语句白名单，能跑什么由账号决定。
package datasource

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"

	"gorm.io/gorm"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// SSH 隧道的验证方式（照 Navicat 的三选一）。
const (
	SSHAuthPassword = "password"
	SSHAuthKey      = "key"
	SSHAuthBoth     = "both"
)

// Sessions 是会话侧的最小依赖：把 MCP 端点的 token 换成会话工作目录
// （决定可见哪些数据源），以及为要挂载工具面的会话备好 token。
//
// 用接口而不是直接 import 会话服务的具体类型，是为了让依赖方向单向：
// service.ChatService 只认得下面的 Mounter 接口，datasource 只认得这个
// Sessions 接口，两个业务包因此不互相 import。
type Sessions interface {
	CwdByMCPToken(ctx context.Context, token string) (string, error)
	EnsureMCPToken(ctx context.Context, sessionID uint) (string, error)
}

// Service 是数据源的业务面。
type Service struct {
	db *gorm.DB
	// workspaceRoot 返回当前工作区根，用于从会话 cwd 推项目名。
	// 用函数而不是值：工作区根在设置页可改，且立刻生效。
	workspaceRoot func() string
	sessions      Sessions
	// mcpBase 是 agent 回连的 MCP 端点前缀（http://127.0.0.1:<port>/api/mcp/db/）。
	mcpBase string
}

func NewService(db *gorm.DB, sessions Sessions, addr string) *Service {
	return &Service{
		db:            db,
		workspaceRoot: service.DefaultCwd,
		sessions:      sessions,
		mcpBase:       mcpBaseURL(addr),
	}
}

// mcpBaseURL 从监听地址推导 MCP 前缀：agent 子进程与我们同机，
// 监听 0.0.0.0 时也走回环回连。
func mcpBaseURL(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "48080"
	}
	return "http://127.0.0.1:" + port + "/api/mcp/db/"
}

// Input 是新建/更新的入参。指针字段表示「没传就不改」——密码留空要能
// 表达「保持原样」，而不是「清空」。
type Input struct {
	Project       string  `json:"project"`
	Env           string  `json:"env"`
	Host          string  `json:"host"`
	Port          int     `json:"port"`
	User          string  `json:"user"`
	Password      *string `json:"password"`
	Database      string  `json:"database"`
	Params        string  `json:"params"`
	Note          string  `json:"note"`
	SSHEnabled    *bool   `json:"sshEnabled"`
	SSHHost       string  `json:"sshHost"`
	SSHPort       int     `json:"sshPort"`
	SSHUser       string  `json:"sshUser"`
	SSHAuth       string  `json:"sshAuth"`
	SSHPassword   *string `json:"sshPassword"`
	SSHKeyPath    string  `json:"sshKeyPath"`
	SSHPassphrase *string `json:"sshPassphrase"`
	ReadOnly      *bool   `json:"readOnly"`
	Disabled      *bool   `json:"disabled"`
}

// List 按项目 + 环境排序分页返回（配置页用，不做项目过滤）。
//
// 分页不是为了「现在」——是为了不给未来留一个随数据量线性变慢的读路径。
// 一次全量返回在几条连接时看不出问题，等到几百条时它已经长在页面加载里了。
func (s *Service) List(ctx context.Context, page, pageSize int, orderBy string) ([]model.DataSource, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	q := s.db.WithContext(ctx).Model(&model.DataSource{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count datasources: %w", err)
	}

	if orderBy == "" {
		orderBy = "project, env"
	}
	var out []model.DataSource
	err := q.Order(orderBy).
		Limit(pageSize).Offset((page - 1) * pageSize).
		Find(&out).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list datasources: %w", err)
	}
	for i := range out {
		decorate(&out[i])
	}
	return out, total, nil
}

// ForCwd 返回某个工作目录所属项目下的数据源——会话侧的**唯一**取数入口。
// onlyEnabled 为 true 时跳过停用的（挂给 AI 的工具面用）。
//
// 推不出项目、或该项目没配数据源，都返回空列表而不是错误：调用方
// （MCP 工具面、斜杠命令）拿到空就该说「这个项目没有可用数据源」，
// 那是正常状态不是故障。
func (s *Service) ForCwd(ctx context.Context, cwd string, onlyEnabled bool) ([]model.DataSource, error) {
	names := projectCandidates(cwd, s.workspaceRoot())
	if len(names) == 0 {
		return nil, nil
	}

	lowered := make([]string, len(names))
	for i, n := range names {
		lowered[i] = strings.ToLower(n)
	}
	q := s.db.WithContext(ctx).Where("LOWER(project) IN ?", lowered)
	if onlyEnabled {
		q = q.Where("disabled = ?", false)
	}
	var out []model.DataSource
	if err := q.Order("env").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list datasources for cwd: %w", err)
	}
	for i := range out {
		decorate(&out[i])
	}
	return out, nil
}

// Resolve 在一批数据源里按引用找出唯一一个。ref 可以写 `<项目>/<环境>`
// 或只写环境名；候选只剩一个时 ref 可以完全省略——会话已经把范围收到
// 一个项目了，多数时候「dev」就足以说清是哪个。
func Resolve(sources []model.DataSource, ref string) (*model.DataSource, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: 当前项目没有可用的数据源", service.ErrNotFound)
	}
	all := refsOf(sources)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if len(sources) == 1 {
			return &sources[0], nil
		}
		return nil, fmt.Errorf("%w: 有多个数据源，请指定其中之一：%s",
			service.ErrInvalid, strings.Join(all, "、"))
	}

	var hits []int
	for i := range sources {
		if strings.EqualFold(ref, sources[i].Ref) || strings.EqualFold(ref, sources[i].Env) {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 1:
		return &sources[hits[0]], nil
	case 0:
		return nil, fmt.Errorf("%w: 没有叫 %q 的数据源，可用的是：%s",
			service.ErrNotFound, ref, strings.Join(all, "、"))
	default:
		matched := make([]string, len(hits))
		for i, h := range hits {
			matched[i] = sources[h].Ref
		}
		return nil, fmt.Errorf("%w: %q 匹配到多个数据源：%s",
			service.ErrInvalid, ref, strings.Join(matched, "、"))
	}
}

func refsOf(sources []model.DataSource) []string {
	out := make([]string, len(sources))
	for i := range sources {
		out[i] = sources[i].Ref
	}
	return out
}

// Get 按 id 取一条（含密码，供连接使用）。
func (s *Service) Get(ctx context.Context, id uint) (*model.DataSource, error) {
	var src model.DataSource
	if err := s.db.WithContext(ctx).First(&src, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("%w: datasource %d", service.ErrNotFound, id)
		}
		return nil, fmt.Errorf("get datasource %d: %w", id, err)
	}
	decorate(&src)
	return &src, nil
}

func (s *Service) Create(ctx context.Context, in Input) (*model.DataSource, error) {
	// 新连接默认只读：要写是明确的一次决定。
	src := model.DataSource{Port: 3306, SSHPort: 22, ReadOnly: true}
	if err := apply(&src, in); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(&src).Error; err != nil {
		return nil, wrapWrite(err, src)
	}
	decorate(&src)
	return &src, nil
}

func (s *Service) Update(ctx context.Context, id uint, in Input) (*model.DataSource, error) {
	src, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := apply(src, in); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Save(src).Error; err != nil {
		return nil, wrapWrite(err, *src)
	}
	decorate(src)
	return src, nil
}

func (s *Service) Delete(ctx context.Context, id uint) error {
	res := s.db.WithContext(ctx).Delete(&model.DataSource{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete datasource %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: datasource %d", service.ErrNotFound, id)
	}
	return nil
}

// ProbeDatabases 列出一组连接参数能看到的全部库——**只给配置页选库用**。
//
// 它是唯一不受「一条连接一个库」约束的读法，因为那时连接还没绑定库。
// 入参是完整连接参数而不是 id：新建连接时还没有 id，总不能逼用户先存
// 一条没填库的记录。id 非零且没给密码时沿用已存的密码（编辑场景）。
func (s *Service) ProbeDatabases(ctx context.Context, id uint, in Input) ([]Database, error) {
	probe := model.DataSource{Port: 3306, SSHPort: 22, Database: "_probe"}
	if id != 0 {
		existing, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		probe = *existing
	}
	if err := apply(&probe, in); err != nil {
		// 库还没选，这一步的「库不能为空」不算错。
		if !strings.Contains(err.Error(), "数据库不能为空") {
			return nil, err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	h, err := connect(ctx, &probe, "")
	if err != nil {
		return nil, err
	}
	defer h.Close()

	const q = `SELECT s.SCHEMA_NAME, s.DEFAULT_CHARACTER_SET_NAME, s.DEFAULT_COLLATION_NAME,
		(SELECT COUNT(*) FROM information_schema.TABLES t WHERE t.TABLE_SCHEMA = s.SCHEMA_NAME)
		FROM information_schema.SCHEMATA s ORDER BY s.SCHEMA_NAME`
	rows, err := h.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("列出数据库失败: %w", err)
	}
	defer rows.Close()

	out := []Database{}
	for rows.Next() {
		var d Database
		var charset, collation sql.NullString
		if err := rows.Scan(&d.Name, &charset, &collation, &d.Tables); err != nil {
			return nil, err
		}
		d.Charset, d.Collation = charset.String, collation.String
		d.System = systemSchemas[strings.ToLower(d.Name)]
		out = append(out, d)
	}
	return out, rows.Err()
}

// ProbeSSH 只拨 SSH 隧道确认跳板机可达、认证有效，不碰后面的 MySQL——
// 给配置页 SSH 页签单独排障用：隧道和库两层分开测，报错才知道卡在哪层。
// probe 模式同 ProbeDatabases：新建时还没有 id，编辑时带 id 沿用已存的
// 密码/通行短语。返回跳板机的版本横幅（SSH-2.0-OpenSSH_… 那行）。
func (s *Service) ProbeSSH(ctx context.Context, id uint, in Input) (string, error) {
	probe := model.DataSource{Port: 3306, SSHPort: 22}
	if id > 0 {
		existing, err := s.Get(ctx, id)
		if err != nil {
			return "", err
		}
		probe = *existing
	}
	// 只合并不做落库校验：测 SSH 不该被「项目/数据库还没填」挡住——
	// 用户就在 SSH 页签上，常规页的必填项与这一层无关。SSH 字段本身
	// 由 dialTunnel/sshAuths 校验。
	merge(&probe, in)
	if !probe.SSHEnabled {
		return "", fmt.Errorf("%w: 先开启 SSH 隧道再测试", service.ErrInvalid)
	}

	// dialTunnel 靠 ctx 的 deadline 保护握手阶段，这里必须给一个。
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	t, err := dialTunnel(ctx, &probe)
	if err != nil {
		return "", err
	}
	defer t.Close()
	return string(t.client.ServerVersion()), nil
}

// Test 拨一次连接确认配置可用，返回服务端版本。
func (s *Service) Test(ctx context.Context, id uint) (string, error) {
	src, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	res, err := Execute(ctx, src, "", "SELECT VERSION()", 1, false)
	if err != nil {
		return "", err
	}
	if len(res.Results) == 0 || res.Results[0].Error != "" {
		return "", fmt.Errorf("连接成功但查询失败: %s", res.Results[0].Error)
	}
	if len(res.Results[0].Rows) > 0 && len(res.Results[0].Rows[0]) > 0 {
		if v, ok := res.Results[0].Rows[0][0].(string); ok {
			return v, nil
		}
	}
	return "", nil
}

// apply 把入参合进记录并校验。
func apply(src *model.DataSource, in Input) error {
	merge(src, in)
	return validate(src)
}

// merge 把入参合进记录。字符串字段整体覆盖（表单每次提交全量），
// 指针字段没传就保持原值。
func merge(src *model.DataSource, in Input) {
	src.Project = strings.TrimSpace(in.Project)
	src.Env = strings.TrimSpace(in.Env)
	src.Host = strings.TrimSpace(in.Host)
	src.User = strings.TrimSpace(in.User)
	src.Database = strings.TrimSpace(in.Database)
	src.Params = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.Params), "?"))
	src.Note = strings.TrimSpace(in.Note)
	src.SSHHost = strings.TrimSpace(in.SSHHost)
	src.SSHUser = strings.TrimSpace(in.SSHUser)
	src.SSHKeyPath = strings.TrimSpace(in.SSHKeyPath)
	src.SSHAuth = strings.TrimSpace(in.SSHAuth)
	if src.SSHAuth == "" {
		src.SSHAuth = SSHAuthPassword
	}
	if in.Port > 0 {
		src.Port = in.Port
	}
	if in.SSHPort > 0 {
		src.SSHPort = in.SSHPort
	}
	// 密码类字段：**空串也视为「不修改」**，不是「清空」。
	//
	// 编辑时后端不下发密码（响应里只有 hasPassword），表单里那一格就是空的；
	// 提交时它会原样发回来一个空串。若按字面意思处理，用户改一次备注就把
	// 生产库密码清了——这个坑踩过一次（探测库列表时连不上才发现）。
	// 真要清空密码，删了重建即可，那种需求罕见到不值得为它冒这个险。
	if in.Password != nil && *in.Password != "" {
		src.Password = *in.Password
	}
	if in.SSHPassword != nil && *in.SSHPassword != "" {
		src.SSHPassword = *in.SSHPassword
	}
	if in.SSHPassphrase != nil && *in.SSHPassphrase != "" {
		src.SSHPassphrase = *in.SSHPassphrase
	}
	if in.SSHEnabled != nil {
		src.SSHEnabled = *in.SSHEnabled
	}
	if in.Disabled != nil {
		src.Disabled = *in.Disabled
	}
	if in.ReadOnly != nil {
		src.ReadOnly = *in.ReadOnly
	}
}

// validate 校验一条完整记录能否落库。
func validate(src *model.DataSource) error {
	switch {
	case src.Project == "":
		return fmt.Errorf("%w: 项目不能为空", service.ErrInvalid)
	case src.Env == "":
		return fmt.Errorf("%w: 环境不能为空", service.ErrInvalid)
	case src.Database == "":
		// 一条连接绑定一个库，这是这套设计的地基（见 model.DataSource）。
		return fmt.Errorf("%w: 数据库不能为空——一条连接只对应一个库", service.ErrInvalid)
	case src.Host == "":
		return fmt.Errorf("%w: 主机不能为空", service.ErrInvalid)
	case src.User == "":
		return fmt.Errorf("%w: 用户名不能为空", service.ErrInvalid)
	case strings.ContainsAny(src.Project, "/\\") || strings.ContainsAny(src.Env, "/\\"):
		// `<项目>/<环境>` 是对外标识的写法，字段里再出现斜杠会让它没法解析。
		return fmt.Errorf("%w: 项目与环境名不能包含斜杠", service.ErrInvalid)
	case src.SSHEnabled && src.SSHHost == "":
		return fmt.Errorf("%w: 开启 SSH 隧道后必须填跳板机地址", service.ErrInvalid)
	case src.SSHAuth != SSHAuthPassword && src.SSHAuth != SSHAuthKey && src.SSHAuth != SSHAuthBoth:
		return fmt.Errorf("%w: 未知的 ssh 验证方式 %q", service.ErrInvalid, src.SSHAuth)
	}
	return nil
}

func decorate(src *model.DataSource) {
	src.Ref = src.Project + "/" + src.Env
	src.HasPassword = src.Password != ""
	src.HasSSHPassword = src.SSHPassword != ""
	src.HasSSHPassphrase = src.SSHPassphrase != ""
}

func wrapWrite(err error, src model.DataSource) error {
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return fmt.Errorf("%w: %s/%s 已存在", service.ErrInvalid, src.Project, src.Env)
	}
	return fmt.Errorf("save datasource: %w", err)
}
