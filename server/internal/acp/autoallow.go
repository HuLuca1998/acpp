package acp

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

// 权限自动放行：技能包是控制端塞给 agent 的只读知识库，住在会话 cwd 之外，
// 于是 agent 每读一个技能参考文件都要弹一次权限。这类请求对用户没有决策
// 价值（技能本来就是我们注入的），却真实制造过死锁——并发两个请求同时挂起、
// 界面只画得下一张卡，没被裁决的那个就永远等下去。
//
// 放行范围刻意收窄：只认 read、只认技能包目录、路径按软链解析后仍在目录内。
// 写操作照常问，agent 改不了技能内容。这比把技能包塞进 additionalDirectories
//（整个目录进工作区、读写全放行、agent 还会把它当自己的工作目录翻）安全得多。

var errRelativePath = errors.New("acp: path is not absolute")

// autoAllowRead 判断这条权限请求是不是「读技能包内的文件」，是则返回该选的
// 选项 ID 与 true。roots 为空（没配技能包，或该方言不支持自动放行）时永不放行。
func autoAllowRead(p RequestPermissionParams, roots []string) (string, bool) {
	if len(roots) == 0 || p.ToolCall.Kind != "read" {
		return "", false
	}
	paths := permissionPaths(p.ToolCall)
	if len(paths) == 0 {
		return "", false
	}
	// 全部路径都得在技能包内：只要有一条在外面，整条请求就交回用户裁决。
	for _, path := range paths {
		if !withinAny(path, roots) {
			return "", false
		}
	}
	return allowOnceOption(p.Options)
}

// permissionPaths 抽出这次调用要碰的文件路径。locations 是 ACP 标准字段
// （claude 的 read/edit 类都带），rawInput.file_path 是 claude Read 工具的参数
// ——两个来源都取，互为兜底；codex 两样都不带，自然拿不到路径也就不会放行。
func permissionPaths(tc PermissionToolCall) []string {
	var out []string
	var locs []struct {
		Path string `json:"path"`
	}
	if len(tc.Locations) > 0 && json.Unmarshal(tc.Locations, &locs) == nil {
		for _, l := range locs {
			if l.Path != "" {
				out = append(out, l.Path)
			}
		}
	}
	var raw struct {
		FilePath string `json:"file_path"`
	}
	if len(tc.RawInput) > 0 && json.Unmarshal(tc.RawInput, &raw) == nil && raw.FilePath != "" {
		out = append(out, raw.FilePath)
	}
	return out
}

// allowOnceOption 从选项里挑「只允许这一次」。按 ACP 标准 kind 找而不是写死
// optionId（claude 的取值是 "allow"，别的 runtime 未必），找不到就不放行。
// 不选 allow_always：那会让 agent 侧落一条持久放行规则，副作用不可控，
// 而每次放行的成本只是一次本地往返。
func allowOnceOption(opts []PermissionOption) (string, bool) {
	for _, o := range opts {
		if o.Kind == "allow_once" && o.OptionID != "" {
			return o.OptionID, true
		}
	}
	return "", false
}

// autoAllowRoots 算出自动放行的根。**两个根都要**：技能包里的
// skills/<name> 是指向技能库 <dataDir>/skills/<name> 的软链（文件系统即启用
// 状态，见 service.SkillService），路径解析软链后落在技能库那一侧，只登记
// skillpack 一个根会全部判不中。两边都按解析后的真实路径登记——判定时也解析，
// 两侧口径必须一致。目录不存在的跳过；都不存在返回空，等于关掉自动放行。
func autoAllowRoots(skillpackDir string) []string {
	if skillpackDir == "" {
		return nil
	}
	var roots []string
	for _, dir := range []string{
		skillpackDir,
		filepath.Join(filepath.Dir(skillpackDir), "skills"),
	} {
		root, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		roots = append(roots, root)
	}
	return roots
}

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		if within(path, root) {
			return true
		}
	}
	return false
}

// within 判断 path 是否落在 root 之内。用 filepath.Rel 而不是字符串前缀，
// 否则 /x/skillpack-evil 会被当成 /x/skillpack 的内部。
func within(path, root string) bool {
	real, err := resolvePath(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, real)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}

// resolvePath 把路径解析成真实路径。相对路径无从判断（agent 的 cwd 未必是
// 会话 cwd），一律拒绝。文件还不存在时退一步解析父目录——既跟随软链，
// 又不会因为读一个不存在的路径就误判；父目录也解析不了就放弃，安全默认。
func resolvePath(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", errRelativePath
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real, nil
	}
	dir, base := filepath.Split(filepath.Clean(p))
	realDir, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", err
	}
	return filepath.Join(realDir, base), nil
}
