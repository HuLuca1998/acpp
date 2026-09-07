package system

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// latestTTL 是最新版查询的缓存时长。体检页一打开就会查一次，缓存避免
// 反复打包管理器的接口；「重新检测」按钮走 refresh 绕过它。
const latestTTL = 5 * time.Minute

// latestChecker 缓存包管理器上的最新版本号。查询要联网，失败一律静默——
// 体检的本地结论（装没装、什么版本）不该被网络状况牵连。
type latestChecker struct {
	mu   sync.Mutex
	at   time.Time
	vers map[string]string
	// brewAPI 可注入以便测试，默认 formulae.brew.sh。
	brewAPI string
}

// versions 返回 依赖 key → 最新版本号。refresh 为真时忽略缓存重查。
// 查询整体失败时沿用上次结果：一次断网不该把已知的版本信息抹掉。
func (c *latestChecker) versions(ctx context.Context, specs []envSpec, refresh bool) map[string]string {
	c.mu.Lock()
	if !refresh && c.vers != nil && time.Since(c.at) < latestTTL {
		cached := c.vers
		c.mu.Unlock()
		return cached
	}
	base := c.brewAPI
	c.mu.Unlock()
	if base == "" {
		base = brewAPI
	}

	got := fetchLatest(ctx, specs, base)

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(got) == 0 {
		return c.vers
	}
	c.vers, c.at = got, time.Now()
	return got
}

// brewAPI 是 Homebrew 的只读包信息接口。查最新版不走本地 `brew info`——
// 那条命令会顺带同步几十兆的包索引，体检页等不起。
const brewAPI = "https://formulae.brew.sh/api"

// fetchLatest 并发查询各项的最新版。整批共用一个超时，慢的那个不拖垮其余；
// 单个失败只是这一项查不到，不影响别的。noLatest 的项直接跳过。
func fetchLatest(ctx context.Context, specs []envSpec, brewBase string) map[string]string {
	// npm 项要先问 npm 自己配的 registry（可能是镜像源），没有 npm 项就
	// 不必为此跑一趟 npm config。
	npmBase := ""
	for _, spec := range specs {
		if !spec.noLatest && spec.installer() == "npm" {
			npmBase = npmRegistry(ctx)
			break
		}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = make(map[string]string, len(specs))
	)
	for _, spec := range specs {
		if spec.noLatest {
			continue
		}
		wg.Go(func() {
			var v string
			switch spec.installer() {
			case "npm":
				v = fetchNpmLatest(fetchCtx, npmBase, spec.npmPkg)
			case "brew":
				v = fetchBrewLatest(fetchCtx, brewBase, spec)
			}
			if v == "" {
				return
			}
			mu.Lock()
			out[spec.key] = v
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

// fetchBrewLatest 读 Homebrew 上的最新版：formula 的版本在 versions.stable，
// cask 的在 version。任何错误都折成空字符串。
func fetchBrewLatest(ctx context.Context, base string, spec envSpec) string {
	kind := "formula"
	if spec.brewCask {
		kind = "cask"
	}
	url := strings.TrimSuffix(base, "/") + "/" + kind + "/" + spec.brewPkg + ".json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		Version  string `json:"version"`
		Versions struct {
			Stable string `json:"stable"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ""
	}
	v := body.Version
	if v == "" {
		v = body.Versions.Stable
	}
	// cask 的版本号常带 ",build" 后缀（"1.2.3,45678"），比较只要前半段。
	v, _, _ = strings.Cut(v, ",")
	return strings.TrimSpace(v)
}

// fetchNpmLatest 读单个包的 dist-tag latest 版本；任何错误都折成空字符串，
// 由调用方按「查不到」处理。
func fetchNpmLatest(ctx context.Context, base, pkg string) string {
	// scoped 包名里的斜杠必须转义，registry 才当成一个包名而非路径。
	url := strings.TrimSuffix(base, "/") + "/" + strings.ReplaceAll(pkg, "/", "%2F") + "/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	// 精简 packument 格式，省掉用不上的 README 等大字段。
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json, application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.Version)
}

// npmRegistry 读 npm 自己配的 registry，尊重用户设的镜像源（配了国内镜像
// 却去打官方源会直接超时）；读不到就回落官方源。
func npmRegistry(ctx context.Context) string {
	const fallback = "https://registry.npmjs.org"
	npm, err := exec.LookPath("npm")
	if err != nil {
		return fallback
	}
	cfgCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cfgCtx, npm, "config", "get", "registry").Output()
	if err != nil {
		return fallback
	}
	v := strings.TrimSpace(string(out))
	if !strings.HasPrefix(v, "http") {
		return fallback
	}
	return v
}

// versionRe 抓点分数字段。各家 --version 的输出格式互不相同，见 parseVersion。
var versionRe = regexp.MustCompile(`\d+(?:\.\d+)+`)

// parseVersion 从 `--version` 的花式输出里抠出版本号——"2.1.258 (Claude Code)"、
// "v22.23.1"、"@agentclientprotocol/codex-acp 1.4.0"、"codex-cli 0.145.0" 都要认，
// 统一取第一段点分数字。抠不出来返回空，调用方据此放弃比较。
func parseVersion(s string) string {
	return versionRe.FindString(s)
}

// compareVersions 逐段比数值，返回 -1/0/1；段数不等时短的补 0。
// 预发布后缀在 parseVersion 阶段就被丢掉了——体检只要「有没有更新」的粗判断。
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}
