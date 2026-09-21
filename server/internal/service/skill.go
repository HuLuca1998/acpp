package service

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// SkillService 管理系统技能库：源目录 <dataDir>/skills 存全部技能，
// 分发目录 <dataDir>/skillpack 只放注入会话的内容。启用状态不进数据库，
// 由 skillpack/skills/<name> 符号链接的存在与否表达——文件系统即状态，
// 手工往源目录放技能同样会被识别。
//
// SKILL.md 的 frontmatter 由这里统一组装与转义，前端只提交结构化字段，
// 不直接编辑 YAML——手写 YAML 一个冒号就能把 frontmatter 弄坏。
type SkillService struct {
	// mu 串行化写操作：启停与删除都是「链接 + 目录」两步，并发交错会留半成品。
	mu      sync.Mutex
	srcDir  string
	packDir string
	// usage 用于删除技能时连带清掉使用计数，可为 nil（测试场景）。
	usage *SkillUsageService
}

func NewSkillService(dataDir string, usage *SkillUsageService) *SkillService {
	return &SkillService{
		srcDir:  filepath.Join(dataDir, "skills"),
		packDir: filepath.Join(dataDir, "skillpack"),
		usage:   usage,
	}
}

// Skill 是列表项；identity 是目录名，frontmatter 的 name 与之保持一致。
type Skill struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// UsageCount 是被 AI 调用的累计次数，由 handler 从 SkillUsageService 合入。
	UsageCount int64 `json:"usageCount"`
}

// SkillDetail 追加 frontmatter 之后的 markdown 正文。
type SkillDetail struct {
	Skill
	Body string `json:"body"`
}

// SkillCreateInput 是创建入参；Body 可空（先建骨架再慢慢写）。
type SkillCreateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// SkillUpdateInput 是更新入参，nil 表示不动该字段。名称不可改——源目录
// 与分发链接都以它为键，改名走删除重建。
type SkillUpdateInput struct {
	Description *string `json:"description"`
	Body        *string `json:"body"`
	Enabled     *bool   `json:"enabled"`
}

// skillNameRe 同时是命名规范与路径安全闸：不匹配的一律拒绝，杜绝穿越。
var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// dashRunRe 压缩归一化后留下的连续连字符。
var dashRunRe = regexp.MustCompile(`-+`)

// ensure 幂等搭好目录骨架。plugin.json 的 name 决定两端技能的显示前缀
// （acpp:<name>）；.agents/skills 链接是 codex extraRoots 的固定发现入口。
func (s *SkillService) ensure() error {
	packSkills := filepath.Join(s.packDir, "skills")
	for _, dir := range []string{s.srcDir, packSkills, filepath.Join(s.packDir, ".claude-plugin"), filepath.Join(s.packDir, ".agents")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("ensure skill dirs: %w", err)
		}
	}
	manifest := filepath.Join(s.packDir, ".claude-plugin", "plugin.json")
	if _, err := os.Lstat(manifest); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(manifest, []byte("{\"name\": \"acpp\"}\n"), 0o644); err != nil {
			return fmt.Errorf("write plugin manifest: %w", err)
		}
	}
	agentsLink := filepath.Join(s.packDir, ".agents", "skills")
	if _, err := os.Lstat(agentsLink); errors.Is(err, os.ErrNotExist) {
		if err := os.Symlink(filepath.Join("..", "skills"), agentsLink); err != nil {
			return fmt.Errorf("link .agents/skills: %w", err)
		}
	}
	return nil
}

func (s *SkillService) List() ([]Skill, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	// codex 侧建的技能落在分发目录里（见 AdoptStrays），列表前先纳管一遍，
	// 否则它们只对会话生效、在页面上永远看不见。收养失败不该挡住列表。
	if err := s.AdoptStrays(); err != nil {
		slog.Warn("adopt stray skills", "err", err)
	}
	entries, err := os.ReadDir(s.srcDir)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}

	skills := make([]Skill, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		skills = append(skills, s.read(e.Name()))
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

func (s *SkillService) Get(name string) (*SkillDetail, error) {
	if err := s.check(name); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.skillFile(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("skill %s: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("read skill %s: %w", name, err)
	}
	doc := parseSkillDoc(string(raw))
	return &SkillDetail{Skill: s.read(name), Body: doc.body}, nil
}

// Create 新建技能目录并组装 SKILL.md，默认启用。
func (s *SkillService) Create(in SkillCreateInput) (*SkillDetail, error) {
	name := strings.TrimSpace(in.Name)
	if !skillNameRe.MatchString(name) {
		return nil, fmt.Errorf("%w: name must be kebab-case (got %q)", ErrInvalid, in.Name)
	}
	desc := strings.TrimSpace(in.Description)
	if desc == "" {
		return nil, fmt.Errorf("%w: description is required", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.srcDir, name)
	if _, err := os.Lstat(dir); err == nil {
		return nil, fmt.Errorf("%w: skill %s already exists", ErrInvalid, name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create skill %s: %w", name, err)
	}
	doc := skillDoc{name: name, description: desc, body: in.Body}
	if err := os.WriteFile(s.skillFile(name), []byte(doc.assemble()), 0o644); err != nil {
		return nil, fmt.Errorf("write skill %s: %w", name, err)
	}
	if err := s.setEnabled(name, true); err != nil {
		return nil, err
	}
	return &SkillDetail{Skill: s.read(name), Body: in.Body}, nil
}

func (s *SkillService) Update(name string, in SkillUpdateInput) (*SkillDetail, error) {
	if err := s.check(name); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.skillFile(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("skill %s: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("read skill %s: %w", name, err)
	}

	if in.Description != nil || in.Body != nil {
		doc := parseSkillDoc(string(raw))
		// name 以目录为准回写：手放的技能若 frontmatter 名字缺失或不一致，
		// 首次编辑即被纠正到唯一事实源上。
		doc.name = name
		if in.Description != nil {
			desc := strings.TrimSpace(*in.Description)
			if desc == "" {
				return nil, fmt.Errorf("%w: description is required", ErrInvalid)
			}
			doc.description = desc
		}
		if in.Body != nil {
			doc.body = *in.Body
		}
		if err := os.WriteFile(s.skillFile(name), []byte(doc.assemble()), 0o644); err != nil {
			return nil, fmt.Errorf("write skill %s: %w", name, err)
		}
	}
	if in.Enabled != nil {
		if err := s.setEnabled(name, *in.Enabled); err != nil {
			return nil, err
		}
	}

	detail, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	return detail, nil
}

func (s *SkillService) Delete(name string) error {
	if err := s.check(name); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.srcDir, name)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("skill %s: %w", name, ErrNotFound)
	}
	// 先摘分发链接再删源目录，反过来会留下指向空处的悬空链接。
	if err := s.setEnabled(name, false); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete skill %s: %w", name, err)
	}
	// 技能没了，使用计数也清掉——否则概览统计会残留指向已删技能的行。
	// 计数是观测数据，清理失败不该让删除半途而废，记日志即可。
	if s.usage != nil {
		if err := s.usage.Delete(name); err != nil {
			slog.Warn("clear skill usage after delete", "skill", name, "err", err)
		}
	}
	return nil
}

// setEnabled 切换分发链接。链接目标用相对路径，数据目录整体迁移后依然有效。
func (s *SkillService) setEnabled(name string, enabled bool) error {
	link := filepath.Join(s.packDir, "skills", name)
	if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unlink skill %s: %w", name, err)
	}
	if !enabled {
		return nil
	}
	if err := os.Symlink(filepath.Join("..", "..", "skills", name), link); err != nil {
		return fmt.Errorf("link skill %s: %w", name, err)
	}
	return nil
}

// AdoptStrays 把分发目录里的游离技能搬回源目录，再建回启用链接。
//
// 起因：codex 自带的 skill-creator 写死把新技能建到 $CODEX_HOME/skills，
// 而技能隔离把那条路径软链到了 skillpack/skills——codex 会话里建的技能
// 于是落成分发目录下的真实目录：管理页看不见（列表只遍历源目录），却对
// 每条会话都已经生效。搬回源目录后页面可编辑可删，启用状态用链接保留：
// 它此前住在分发目录里，本就等于启用。
//
// 点开头的项跳过——那是 codex 自己铺的系统技能（.system）与标记文件，
// 属于它的运行数据，不是用户技能。
func (s *SkillService) AdoptStrays() error {
	if err := s.ensure(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	packSkills := filepath.Join(s.packDir, "skills")
	entries, err := os.ReadDir(packSkills)
	if err != nil {
		return fmt.Errorf("scan skillpack: %w", err)
	}
	for _, e := range entries {
		// 符号链接是正常的启用状态（ReadDir 不跟随，链接的 IsDir 为假）。
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := s.freeSkillName(slugSkillName(e.Name()))
		if name == "" {
			slog.Warn("stray skill not adoptable", "dir", e.Name())
			continue
		}
		if err := os.Rename(filepath.Join(packSkills, e.Name()), filepath.Join(s.srcDir, name)); err != nil {
			return fmt.Errorf("adopt stray skill %s: %w", e.Name(), err)
		}
		// 搬走后原位置空出来，补回链接，注入面对 agent 不中断。
		if err := s.setEnabled(name, true); err != nil {
			return err
		}
		slog.Info("adopted stray skill", "dir", e.Name(), "name", name)
	}
	return nil
}

// slugSkillName 把外部来源的目录名归一到技能命名规范（kebab-case）：大写
// 转小写，下划线、空格、点转连字符，其余字符丢弃。归一不出合法名字时返回
// 空串——名字就是路径，不能凑。
func slugSkillName(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == ' ', r == '.':
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(dashRunRe.ReplaceAllString(b.String(), "-"), "-")
	if !skillNameRe.MatchString(slug) {
		return ""
	}
	return slug
}

// freeSkillName 在源目录里找一个没被占用的名字。同名技能已存在时加 -codex
// 后缀（游离技能只会来自 codex 侧），仍冲突则续加序号——绝不覆盖用户已有
// 的技能。实在找不到返回空串，由调用方跳过。
func (s *SkillService) freeSkillName(name string) string {
	if name == "" {
		return ""
	}
	candidate := name
	for i := 0; i < 20; i++ {
		if _, err := os.Lstat(filepath.Join(s.srcDir, candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		if i == 0 {
			candidate = name + "-codex"
			continue
		}
		candidate = fmt.Sprintf("%s-codex-%d", name, i+1)
	}
	return ""
}

// read 尽力解析一个技能目录：frontmatter 坏了也要出现在列表里让用户修，
// 所以除名字外的字段都容错为空值。
func (s *SkillService) read(name string) Skill {
	sk := Skill{Name: name}
	if info, err := os.Stat(s.skillFile(name)); err == nil {
		sk.UpdatedAt = info.ModTime()
	}
	if raw, err := os.ReadFile(s.skillFile(name)); err == nil {
		sk.Description = parseSkillDoc(string(raw)).description
	}
	if _, err := os.Lstat(filepath.Join(s.packDir, "skills", name)); err == nil {
		sk.Enabled = true
	}
	return sk
}

func (s *SkillService) skillFile(name string) string {
	return filepath.Join(s.srcDir, name, "SKILL.md")
}

// check 校验路径段安全性；不通过一律按 404 处理，不泄漏目录结构。
func (s *SkillService) check(name string) error {
	if !skillNameRe.MatchString(name) {
		return fmt.Errorf("skill %s: %w", name, ErrNotFound)
	}
	return nil
}

// skillDoc 是 SKILL.md 的结构化视图。extra 保留 name/description 之外的
// frontmatter 行（如第三方技能的 license），编辑不弄丢别人的字段。
type skillDoc struct {
	name        string
	description string
	body        string
	extra       []string
}

// assemble 组装 SKILL.md。description 一律双引号转义——技能描述里冒号、
// 井号是常态，裸写会破坏 YAML。
func (d skillDoc) assemble() string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: \"%s\"\n", d.name, yamlEscape(d.description))
	for _, line := range d.extra {
		fmt.Fprintln(&b, line)
	}
	b.WriteString("---\n")
	if body := strings.TrimSpace(d.body); body != "" {
		fmt.Fprintf(&b, "\n%s\n", body)
	}
	return b.String()
}

func yamlEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	// frontmatter 是单行值，换行折成空格而不是让它撑破字段。
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// parseSkillDoc 解析 SKILL.md。只支持单行 frontmatter 值——技能规范要求
// description 单行写全触发场景，这里不实现完整 YAML；认不出的行进 extra
// 原样保留。没有 frontmatter 时整个文件当 body。
func parseSkillDoc(content string) skillDoc {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return skillDoc{body: strings.TrimSpace(content)}
	}

	doc := skillDoc{}
	rest := 1
	closed := false
	for i, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			rest = i + 2
			closed = true
			break
		}
		if after, ok := strings.CutPrefix(trimmed, "name:"); ok {
			doc.name = strings.Trim(strings.TrimSpace(after), `"'`)
		} else if after, ok := strings.CutPrefix(trimmed, "description:"); ok {
			doc.description = yamlUnescape(strings.TrimSpace(after))
		} else if trimmed != "" {
			doc.extra = append(doc.extra, line)
		}
	}
	if !closed {
		// frontmatter 没闭合：别把半截 YAML 当正文，全文保底为 body。
		return skillDoc{body: strings.TrimSpace(content)}
	}
	doc.body = strings.TrimSpace(strings.Join(lines[rest:], "\n"))
	return doc
}

func yamlUnescape(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		s = s[1 : len(s)-1]
		s = strings.ReplaceAll(s, `\"`, `"`)
		s = strings.ReplaceAll(s, `\\`, `\`)
		return s
	}
	return strings.Trim(s, `'`)
}
