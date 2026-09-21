package service

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
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

// skillImportMaxBytes 是导入包解开后的总量上限。技能是给模型读的文本加少量
// 模板，超过这个量说明包里装的不是技能——挡的是解压炸弹。
const skillImportMaxBytes int64 = 32 << 20

// ExportZip 把技能打成 zip 写进 w：name 为空导出整个技能库（换设备搬家用），
// 否则只导出那一个。包内结构就是技能库的结构（`<name>/SKILL.md` + 附属
// 文件），到新机器上导入即原样落回。
func (s *SkillService) ExportZip(name string, w io.Writer) error {
	if err := s.ensure(); err != nil {
		return err
	}
	var names []string
	if name != "" {
		if err := s.check(name); err != nil {
			return err
		}
		if _, err := os.Stat(s.skillFile(name)); err != nil {
			return fmt.Errorf("skill %s: %w", name, ErrNotFound)
		}
		names = []string{name}
	} else {
		list, err := s.List()
		if err != nil {
			return err
		}
		for _, sk := range list {
			names = append(names, sk.Name)
		}
		if len(names) == 0 {
			return fmt.Errorf("%w: the skill library is empty", ErrNotFound)
		}
	}

	zw := zip.NewWriter(w)
	var total int64
	for _, n := range names {
		// 复用工作区打包那对函数：跳过点文件与符号链接的规矩在那边，技能
		// 目录要的正是同一套。
		if err := zipAddDir(zw, filepath.Join(s.srcDir, n), n, &total); err != nil {
			return fmt.Errorf("pack skill %s: %w", n, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finish skill archive: %w", err)
	}
	return nil
}

// SkillImportResult 是导入结果：进来了哪些、跳过了哪些。
type SkillImportResult struct {
	Imported []string          `json:"imported"`
	Skipped  []SkillImportSkip `json:"skipped"`
}

// SkillImportSkip 是一条没能导入的技能。Reason 是原因码（`exists` /
// `invalid_name` / `no_doc`），用户可见的文案由前端按语言给。
type SkillImportSkip struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// ImportZip 从 zip 还原技能到源目录，**一律停用**：技能会注入每条会话，
// 导入回来的内容得由人在页面上看过再打开。已存在的同名技能跳过而不是覆盖
// ——那是用户自己的东西，只由用户自己改。名字归一到 kebab-case。
func (s *SkillService) ImportZip(r io.ReaderAt, size int64) (*SkillImportResult, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: not a zip archive (%s)", ErrInvalid, err)
	}
	groups, err := groupSkillEntries(zr.File)
	if err != nil {
		return nil, err
	}
	if err := s.ensure(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	res := &SkillImportResult{Imported: []string{}, Skipped: []SkillImportSkip{}}
	tops := make([]string, 0, len(groups))
	for top := range groups {
		tops = append(tops, top)
	}
	sort.Strings(tops)

	var total int64
	for _, top := range tops {
		entries := groups[top]
		name := slugSkillName(top)
		switch {
		case name == "":
			res.Skipped = append(res.Skipped, SkillImportSkip{Name: top, Reason: "invalid_name"})
			continue
		case !hasSkillDoc(entries):
			// 没有 SKILL.md 的目录不是技能——可能是包里夹带的别的东西。
			res.Skipped = append(res.Skipped, SkillImportSkip{Name: name, Reason: "no_doc"})
			continue
		}
		if _, err := os.Lstat(filepath.Join(s.srcDir, name)); err == nil {
			res.Skipped = append(res.Skipped, SkillImportSkip{Name: name, Reason: "exists"})
			continue
		}
		if err := s.writeSkillEntries(name, entries, &total); err != nil {
			return nil, err
		}
		res.Imported = append(res.Imported, name)
	}
	return res, nil
}

// skillZipEntry 是 zip 里一条属于某个技能的文件。
type skillZipEntry struct {
	rel  string // 技能目录内的相对路径（'/' 分隔）
	file *zip.File
}

// groupSkillEntries 把 zip 条目按顶层目录（= 技能名）分组。
//
// 路径一律 path.Clean 后校验：带 `..` 或绝对路径的条目让整包失败，不是跳过
// ——一条 `../../.ssh/authorized_keys` 就能写到技能库外面去（zip slip），
// 出现它说明这个包不可信。目录条目、点开头的段与顶层散文件跳过。
func groupSkillEntries(files []*zip.File) (map[string][]skillZipEntry, error) {
	groups := map[string][]skillZipEntry{}
	for _, f := range files {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		// zip 规范里分隔符是 '/'，但 Windows 上打的包见过反斜杠。
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("%w: archive escapes the skill library (%s)", ErrInvalid, f.Name)
		}
		segs := strings.Split(name, "/")
		if len(segs) < 2 {
			continue
		}
		stray := false
		for _, seg := range segs {
			if seg == "" || strings.HasPrefix(seg, ".") {
				stray = true
			}
		}
		if stray {
			continue
		}
		groups[segs[0]] = append(groups[segs[0]], skillZipEntry{
			rel:  path.Join(segs[1:]...),
			file: f,
		})
	}
	return groups, nil
}

func hasSkillDoc(entries []skillZipEntry) bool {
	for _, e := range entries {
		if e.rel == "SKILL.md" {
			return true
		}
	}
	return false
}

// writeSkillEntries 把一个技能的条目解压进源目录，并把 frontmatter 的 name
// 对齐到目录名。总量边写边算：解压炸弹不能靠「写完再看多大」来防。
func (s *SkillService) writeSkillEntries(name string, entries []skillZipEntry, total *int64) error {
	dir := filepath.Join(s.srcDir, name)
	for _, e := range entries {
		*total += int64(e.file.UncompressedSize64)
		if *total > skillImportMaxBytes {
			return fmt.Errorf("%w: archive is too large (limit %d MiB)", ErrInvalid, skillImportMaxBytes>>20)
		}
		target := filepath.Join(dir, filepath.FromSlash(e.rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create dir for skill %s: %w", name, err)
		}
		rc, err := e.file.Open()
		if err != nil {
			return fmt.Errorf("read %s from archive: %w", e.file.Name, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, skillImportMaxBytes))
		rc.Close()
		if err != nil {
			return fmt.Errorf("read %s from archive: %w", e.file.Name, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("write skill file %s: %w", target, err)
		}
	}

	// 两端都按目录名发现技能，frontmatter 的 name 不一致会让「列表里叫 A、
	// AI 眼里叫 B」。导入时顺手纠正，不必等用户首次编辑。
	raw, err := os.ReadFile(s.skillFile(name))
	if err != nil {
		return fmt.Errorf("read imported skill %s: %w", name, err)
	}
	doc := parseSkillDoc(string(raw))
	if doc.name == name {
		return nil
	}
	doc.name = name
	if err := os.WriteFile(s.skillFile(name), []byte(doc.assemble()), 0o644); err != nil {
		return fmt.Errorf("write imported skill %s: %w", name, err)
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
