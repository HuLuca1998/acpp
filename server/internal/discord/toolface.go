package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"acpp/server/internal/mcp"
	"acpp/server/internal/schedule"
)

// acpp-chat 工具面：agent 把成果**交到用户手上**的出口，外加交出去之后
// 的收回权。网页会话里成果摊开靠预览面板；discord 的等价物是「东西直接
// 出现在频道里」——要么是附件，要么是一条点开即看的链接。
//
// 三个工具是一套：send_file 交付，list_links 看还有什么挂在外面，
// revoke_link 收回。只给第一个的话，外链就成了发出去再也收不回的东西
// （用户点名要过这条：「我看完之后告知 ai 删除链接」）。
//
// 工具面由本包自己声明与执行（凭证/路径护栏/发送都在这），协议外壳复用
// internal/mcp，交付形态的实现在 deliver.go。

const chatServerName = "acpp-chat"

const (
	chatToolSend   = "send_file"
	chatToolLinks  = "list_links"
	chatToolRevoke = "revoke_link"
)

// chatMounts 为一个子区会话算自家工具面的挂载载荷。
func (s *Service) chatMounts(threadID string, b Binding) (servers []any, meta map[string]any, err error) {
	if s.deps.MCPBase == "" {
		return nil, nil, nil
	}
	token, err := s.chatTok.Issue(threadID, b.Workdir, 0)
	if err != nil {
		return nil, nil, err
	}
	url := strings.TrimRight(s.deps.MCPBase, "/") + "/" + token
	// 定时任务面与交付面同一枚凭证，端点多一个 -cron 段（见 cronURL）。
	withCron := s.sched != nil
	if b.Agent == "claude" {
		mcpServers := map[string]any{
			chatServerName: map[string]any{"type": "http", "url": url},
		}
		allowed := chatAllowedTools()
		if withCron {
			mcpServers[cronServerName] = map[string]any{"type": "http", "url": cronURL(url)}
			allowed = append(allowed, cronAllowedTools()...)
		}
		return nil, map[string]any{
			"claudeCode": map[string]any{"options": map[string]any{
				"mcpServers":   mcpServers,
				"allowedTools": allowed,
			}},
		}, nil
	}
	servers = []any{map[string]any{
		"type": "http", "name": chatServerName, "url": url, "headers": []any{},
	}}
	if withCron {
		servers = append(servers, map[string]any{
			"type": "http", "name": cronServerName, "url": cronURL(url), "headers": []any{},
		})
	}
	return servers, nil, nil
}

// chatAllowedTools 是 claude 侧预批的工具名。交付是「把东西给用户」的最后
// 一步，在这一步弹权限卡，用户在点之前什么都拿不到。
func chatAllowedTools() []string {
	var out []string
	for _, t := range []string{chatToolSend, chatToolLinks, chatToolRevoke} {
		out = append(out, "mcp__"+chatServerName+"__"+t)
	}
	return out
}

// HandleChatMCP 处理一条发到 /api/mcp/discord/{token} 的 JSON-RPC 消息。
func (s *Service) HandleChatMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	srv := mcp.Server{
		Name: chatServerName,
		Resolve: func(ctx context.Context, token string) ([]mcp.Tool, error) {
			threadID, cwd, _, ok := s.chatTok.Lookup(token)
			if !ok {
				return nil, fmt.Errorf("凭证无效（会话可能已重启）")
			}
			return s.chatTools(threadID, cwd), nil
		},
	}
	return srv.Serve(ctx, token, raw)
}

// sendDescription 是交付能力的唯一触发器，写给模型看。它要回答的不是
// 「这个工具怎么调」，而是「什么时候该想起它、该选哪种形态」——用户开口
// 要东西时，模型的第一反应往往是把文件内容读一遍贴进对话。
const sendDescription = "把工作目录里的文件交到用户手上（当前 Discord 对话）。" +
	"用户不在你的机器上：贴路径他点不开，把内容读一遍粘出来也不等于给了他文件。" +
	"什么时候用：用户开口要东西（「把 xxx 发上来」「那份报告给我」「日志发我看看」），" +
	"或你产出了报告、图表、图片、数据文件需要交付。" +
	"\n形态由 as 决定：" +
	"\n- **用户点名了某个文件（「把 tmp/日报.html 发上来」这种带路径或文件名的），一律 as=file**：" +
	"他指名要的是那份文件本身——能存档、能转发、能再打开的那一个，不是它的展示形态。" +
	"这一条优先于下面所有默认" +
	"\n- auto（默认）：你自己产出、用户没点名具体文件时用。.html 发成**渲染后的外链**" +
	"（手机上点开就是排好版的页面），其余直接发文件" +
	"\n- file：作为附件上传原文件。用户说「文件」「原件」「别发链接」时也用这个" +
	"\n- link：发成外链（.md 与代码给 gist 页面，那里本来就有渲染与高亮）" +
	"\n- image：整页长图。用户要「截图」，或内容不便上外网时用" +
	"\n外链是不公开列出的 GitHub secret gist，但**拿到链接的人都能打开**：" +
	"涉密内容（凭据、个人信息、未公开数据）一律用 as=file，别图省事。" +
	"expire 定有效期（默认 7d，可写 12h / 30d / never）。" +
	"paths 一次最多 10 个，相对工作目录；caption 是随件的一句话说明。"

const linksDescription = "列出本对话还挂在外面的链接（发过 as=link 的那些）：标题、id、地址、什么时候到期。" +
	"用户问「我那个链接还在吗」「都发过哪些链接」，或你要撤销却手上没有 id 时用。" +
	"**要点：链接卡上有「立即失效」按钮，用户可以自己点，那不经过你**——" +
	"所以在说某条链接「还有效」之前先用这个工具核一遍，别凭对话记忆下结论。"

const revokeDescription = "撤销外链：删掉对应的 gist，外网立刻打不开。" +
	"用户说「看完了」「可以删了」「把链接撤了」时用——链接默认挂 7 天，他说不用了就别让它继续挂着。" +
	"link 传发布时给你的 id（整条链接地址也认），或传 all 撤销本对话的全部外链。" +
	"只能撤 acpp 自己发的链接，用户账号里别的 gist 碰不到。"

// chatTools 构造子区会话可用的工具集。
func (s *Service) chatTools(threadID, cwd string) []mcp.Tool {
	return []mcp.Tool{{
		Name:        chatToolSend,
		Description: sendDescription,
		// 只读注解：交付不改工作目录里的任何文件。
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "文件路径，相对工作目录；一次最多 10 个",
				},
				"as": map[string]any{
					"type": "string",
					"enum": []any{deliverAuto, deliverFile, deliverLink, deliverImage},
					"description": "交付形态，默认 auto（.html 走外链、其余走文件）。" +
						"用户明说要文件就传 file，要截图传 image",
				},
				"caption": map[string]any{"type": "string", "description": "可选，随件的一句话说明"},
				"expire": map[string]any{
					"type":        "string",
					"description": "可选，外链有效期，默认 7d；可写 12h / 30d / never。只对外链生效",
				},
			},
			"required": []any{"paths"},
		},
		Call: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Paths []string `json:"paths"`
				// Path 是单数写法的兼容口：模型照着「一个文件」的直觉传
				// path 的概率不低，为此报错纯属自找麻烦。
				Path    string `json:"path"`
				As      string `json:"as"`
				Caption string `json:"caption"`
				Expire  string `json:"expire"`
			}
			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}
			paths := in.Paths
			if strings.TrimSpace(in.Path) != "" {
				paths = append(paths, in.Path)
			}
			return s.deliverFiles(ctx, threadID, cwd, paths, in.Caption, in.As, in.Expire)
		},
	}, {
		Name:        chatToolLinks,
		Description: linksDescription,
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.listLinks(ctx, threadID)
		},
	}, {
		Name:        chatToolRevoke,
		Description: revokeDescription,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"link": map[string]any{
					"type":        "string",
					"description": "外链 id（整条链接地址也认），或 all 撤销本对话全部外链",
				},
			},
			"required": []any{"link"},
		},
		Call: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Link string `json:"link"`
				// ID 是同义写法的兼容口，理由同 send_file 的 path。
				ID string `json:"id"`
			}
			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}
			return s.revokeLinks(ctx, threadID, orDefault(in.Link, in.ID))
		},
	}}
}

func decodeArgs(args json.RawMessage, out any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, out); err != nil {
		return fmt.Errorf("参数解析失败：%w", err)
	}
	return nil
}

// resolveInWorkdir 把路径解析成确认落在工作目录内的绝对路径（软链先
// 解析再比对，护栏口径与 ReportPath 一致）。
func resolveInWorkdir(workdir, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("path 不能为空")
	}
	abs := rel
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workdir, rel)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("文件不存在：%s", rel)
	}
	root, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", fmt.Errorf("解析工作目录: %w", err)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf("路径在工作目录之外")
	}
	if info, err := os.Stat(resolved); err != nil || info.IsDir() {
		return "", fmt.Errorf("不是一个文件：%s", rel)
	}
	return resolved, nil
}

// ---- acpp-cron 工具面：agent 在对话里自建定时任务 ----
//
// 与 acpp-chat 共用同一枚回连凭证（token → 子区 → 频道），只是 server 名
// 单列成 acpp-cron：工具台按 server 分组，模型读工具清单时也一眼分得清
// 「交付」与「定时」两件事。投递目标固定为当前频道——从凭证推，不让模型
// 填 channel id（openclaw 的 resolveCronCreationDelivery 同一取舍）。

const cronServerName = "acpp-cron"

const (
	cronToolAdd    = "cron_add"
	cronToolList   = "cron_list"
	cronToolUpdate = "cron_update"
	cronToolRemove = "cron_remove"
)

// cronURL 由交付面的回连地址推定时面的：同一 token，路径段多一个 -cron。
func cronURL(chatURL string) string {
	return strings.Replace(chatURL, "/api/mcp/discord/", "/api/mcp/discord-cron/", 1)
}

// cronAllowedTools 是 claude 侧预批的工具名。建任务是用户在对话里明确
// 要的事，在这一步弹权限卡只会打断「说一句就建好」的体验。
func cronAllowedTools() []string {
	var out []string
	for _, t := range []string{cronToolAdd, cronToolList, cronToolUpdate, cronToolRemove} {
		out = append(out, "mcp__"+cronServerName+"__"+t)
	}
	return out
}

// 工具描述写给模型看，回答的是「什么时候该想起它」与「prompt 该写成什么样」
// ——后者是整个功能成败的关键：任务到点在全新会话里跑，没有这次对话的
// 任何记忆，写得像「像刚才那样」的任务第一次就会跑歪。
// cronAddDesc 是 cron_add 的说明。带上生成时刻：模型没有钟表，「2 小时后」
// 靠 in 由服务端换算，「明早 9 点」这类具体钟点它得知道今天是几号才算得
// 出 at。工具清单在会话开头拉一次，这个时刻就是会话起点——跨日的长会话
// 里日期可能已经翻篇，所以描述里也让它优先用 in。
func cronAddDesc(now time.Time) string {
	return cronAddDescription + "\n**现在是 " + now.Format("2006-01-02 15:04（MST）") +
		"**（会话开始时刻）。相对时间——「2 小时后」「40 分钟后」「明天这个点」——一律用 in（2h / 40m / 1d），" +
		"别自己换算 at；只有用户给了具体钟点（「明早 9 点」「9 月 10 日 15:00」）才用 at，以上面这个时刻为今天的基准。"
}

const cronAddDescription = "给当前频道建一条定时任务：到点后在**全新会话**里执行 prompt，成果发到这个频道" +
	"（频道里一条起始消息 + 挂在下面的子区，跑完用户可以在子区里追问）。" +
	"\n什么时候用：用户说「每天 / 每周一 / 每隔 N 小时 / 以后定时 / 到点 / 固定时间发一份」" +
	"——建任务。**绝不要用 sleep 循环、反复轮询或让用户手动提醒来冒充定时。**" +
	"\n时间：cron 是 5 段表达式（分 时 日 月 周），按 tz 的墙钟解释，tz 缺省是本机时区。" +
	"「每天早上 10 点」→ cron=\"0 10 * * *\"；「每两小时」→ \"0 */2 * * *\"；「每周一 9 点」→ \"0 9 * * 1\"；" +
	"「每个工作日 18:30」→ \"30 18 * * 1-5\"。只跑一次的用 in（相对时长）或 at（RFC3339 时刻）代替 cron，跑成功即自动删。" +
	"\n**prompt 是那次运行的全部上下文**：新会话没有这次对话的任何记忆。把数据来源（哪个库哪些表 / 哪台机器" +
	"哪些日志路径）、口径（时间窗按「运行时刻」相对表述，如「过去 7 天」「自上次运行以来」；指标定义；过滤条件）、" +
	"输出结构（先一行结论，再要点）、交付形态（报告按 html-report 写成单文件并 report_open；文件用 send_file）、" +
	"静默条件（巡检类：没有值得汇报的内容只回 NO_REPORT）全部写进去；不要写「像刚才那样」「同上」。" +
	"正确的做法是先在当前对话里把这件事做过一次、口径对齐了，再把**刚才实际走过的步骤**写成 prompt。" +
	"\n建完把「什么时候、干什么、多久一次、发到哪」用一句话复述给用户确认；任务卡上有按钮可以立即运行、停用、删除。"

const cronListDescription = "列出当前频道的定时任务：id、名字、时间、下次运行、上次结果。" +
	"用户问「有哪些定时任务」「那个日报还在跑吗」「上次跑成功了吗」，或你要改 / 删任务却手上没有 id 时用。"

const cronUpdateDescription = "改当前频道的一条定时任务：换时间（cron / tz / at / in）、改 prompt、停用或启用（enabled）。" +
	"只传要改的字段。用户说「改成每天 9 点」「先停掉」「把周报也加上 xx 指标」时用；手上没有 id 先 cron_list。" +
	"改 prompt 时传完整的新版本，不是补丁。"

const cronRemoveDescription = "删掉当前频道的一条定时任务（不可恢复，prompt 一并没了）。" +
	"用户明确说「删掉 / 不用了 / 取消这个定时」时用；只是暂时不跑用 cron_update 停用。"

// HandleCronMCP 处理一条发到 /api/mcp/discord-cron/{token} 的 JSON-RPC 消息。
func (s *Service) HandleCronMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	srv := mcp.Server{
		Name: cronServerName,
		Resolve: func(ctx context.Context, token string) ([]mcp.Tool, error) {
			threadID, _, _, ok := s.chatTok.Lookup(token)
			if !ok {
				return nil, fmt.Errorf("凭证无效（会话可能已重启）")
			}
			return s.cronTools(threadID), nil
		},
	}
	return srv.Serve(ctx, token, raw)
}

// jobScope 由子区推任务归属（频道 id）与创建者（最近说话的人）。
func (s *Service) jobScope(threadID string) (channelID, creator string, err error) {
	if s.sched == nil {
		return "", "", fmt.Errorf("定时任务未启用")
	}
	t, ok := s.store.config().thread(threadID)
	if !ok {
		return "", "", fmt.Errorf("这个子区不属于任何绑定频道")
	}
	tc := s.chatState(threadID)
	tc.mu.Lock()
	creator = tc.lastUser
	tc.mu.Unlock()
	return t.ChannelID, creator, nil
}

// cronArgs 是 cron_add / cron_update 共用的入参形状。
type cronArgs struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Cron string `json:"cron"`
	TZ   string `json:"tz"`
	At   string `json:"at"`
	// In 是相对时长（2h / 90m / 1d）：服务端拿当前时刻加出来的一次性 at。
	In      string `json:"in"`
	Prompt  string `json:"prompt"`
	Enabled *bool  `json:"enabled"`
}

// atTime 把 at（绝对 RFC3339）或 in（相对时长）折成一次性时刻。in 是给
// 「2 小时后」「明天这个点」准备的：模型不知道现在几点，让它自己算绝对
// 时刻十有八九算错（真机里见过算到昨天的），相对时长由服务端按 now 加出
// 来才靠得住。
func (a cronArgs) atTime(now time.Time) (*time.Time, error) {
	at, in := strings.TrimSpace(a.At), strings.TrimSpace(a.In)
	switch {
	case at != "" && in != "":
		return nil, fmt.Errorf("at 与 in 只能二选一")
	case in != "":
		d, err := parseDelay(in)
		if err != nil {
			return nil, err
		}
		t := now.Add(d)
		return &t, nil
	case at != "":
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return nil, fmt.Errorf("at 要是 RFC3339 时刻（如 2026-09-04T09:00:00+08:00）：%w", err)
		}
		return &t, nil
	}
	return nil, nil
}

// parseDelay 解析相对时长：Go duration 语法（2h、90m、1h30m），另认 d 作
// 天——Go 原生不认「1d」，而「明天这个时候」正是模型爱用的说法。
func parseDelay(s string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if days, err := strconv.Atoi(n); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("in 要是正的时长，如 2h、90m、1h30m、1d：%q 不认识", s)
	}
	return d, nil
}

// cronTools 构造子区会话可用的定时任务工具集。
func (s *Service) cronTools(threadID string) []mcp.Tool {
	jobSchema := map[string]any{
		"name":   map[string]any{"type": "string", "description": "任务名（≤80 字），起始消息与子区标题都用它"},
		"cron":   map[string]any{"type": "string", "description": "5 段 cron 表达式（分 时 日 月 周），与 at 二选一"},
		"tz":     map[string]any{"type": "string", "description": "IANA 时区（如 Asia/Shanghai），缺省本机时区"},
		"at":     map[string]any{"type": "string", "description": "一次性任务的 RFC3339 时刻（用户给了具体钟点时用），与 cron / in 三选一；跑成功即删"},
		"in":     map[string]any{"type": "string", "description": "相对时长后跑一次（2h / 90m / 1h30m / 1d），与 cron / at 三选一；服务端按当前时刻换算，「N 小时后」一律用它"},
		"prompt": map[string]any{"type": "string", "description": "任务提示词：那次运行的全部上下文，必须自包含"},
	}
	updateSchema := map[string]any{"id": map[string]any{"type": "string", "description": "任务 id（cron_list 里的）"}}
	for k, v := range jobSchema {
		updateSchema[k] = v
	}
	updateSchema["enabled"] = map[string]any{"type": "boolean", "description": "false 停用、true 启用"}

	now := time.Now()
	if loc, err := time.LoadLocation(schedule.HostTZ()); err == nil {
		now = now.In(loc)
	}
	return []mcp.Tool{{
		Name:        cronToolAdd,
		Description: cronAddDesc(now),
		InputSchema: map[string]any{
			"type": "object", "properties": jobSchema, "required": []any{"name", "prompt"},
		},
		Call: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in cronArgs
			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}
			channelID, creator, err := s.jobScope(threadID)
			if err != nil {
				return "", err
			}
			at, err := in.atTime(time.Now())
			if err != nil {
				return "", err
			}
			tz := in.TZ
			if tz == "" {
				tz = schedule.HostTZ()
			}
			job, err := s.sched.Add(schedule.Input{
				Scope: channelID, Name: in.Name, Cron: in.Cron, TZ: tz, At: at,
				Prompt: in.Prompt, CreatedBy: creator,
			})
			if err != nil {
				return "", jobErr(err)
			}
			s.postJobCard(s.store.config().BotToken, threadID, job)
			return "已创建定时任务：" + jobText(job) +
				"\n成果会发到本频道（起始消息 + 子区）。请把「什么时候、干什么、多久一次」复述给用户确认；任务卡已发在子区，上面有立即运行 / 停用 / 删除按钮。", nil
		},
	}, {
		Name:        cronToolList,
		Description: cronListDescription,
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(ctx context.Context, _ json.RawMessage) (string, error) {
			channelID, _, err := s.jobScope(threadID)
			if err != nil {
				return "", err
			}
			jobs := s.sched.Jobs(channelID)
			if len(jobs) == 0 {
				return "本频道还没有定时任务。", nil
			}
			lines := make([]string, 0, len(jobs))
			for _, j := range jobs {
				lines = append(lines, "- "+jobText(j))
			}
			return strings.Join(lines, "\n"), nil
		},
	}, {
		Name:        cronToolUpdate,
		Description: cronUpdateDescription,
		InputSchema: map[string]any{"type": "object", "properties": updateSchema, "required": []any{"id"}},
		Call: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in cronArgs
			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}
			channelID, _, err := s.jobScope(threadID)
			if err != nil {
				return "", err
			}
			job, ok := s.sched.Get(in.ID)
			if !ok || job.Scope != channelID {
				return "", fmt.Errorf("本频道没有 id 为 %s 的任务（先 cron_list）", in.ID)
			}
			at, err := in.atTime(time.Now())
			if err != nil {
				return "", err
			}
			p := schedule.Patch{At: at, Enabled: in.Enabled}
			if in.Name != "" {
				p.Name = &in.Name
			}
			if in.Cron != "" {
				p.Cron = &in.Cron
			}
			if in.TZ != "" {
				p.TZ = &in.TZ
			}
			if in.Prompt != "" {
				p.Prompt = &in.Prompt
			}
			job, err = s.sched.Update(in.ID, p)
			if err != nil {
				return "", jobErr(err)
			}
			s.postJobCard(s.store.config().BotToken, threadID, job)
			return "已更新：" + jobText(job), nil
		},
	}, {
		Name:        cronToolRemove,
		Description: cronRemoveDescription,
		Annotations: &mcp.Annotations{DestructiveHint: true},
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string", "description": "任务 id"}},
			"required":   []any{"id"},
		},
		Call: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in cronArgs
			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}
			channelID, _, err := s.jobScope(threadID)
			if err != nil {
				return "", err
			}
			job, ok := s.sched.Get(in.ID)
			if !ok || job.Scope != channelID {
				return "", fmt.Errorf("本频道没有 id 为 %s 的任务（先 cron_list）", in.ID)
			}
			if err := s.sched.Remove(in.ID); err != nil {
				return "", jobErr(err)
			}
			s.say(ctx, s.store.config().BotToken, threadID, "🗑 定时任务 **"+trimRunes(job.Name, 80)+"** 已删除。")
			return "已删除定时任务「" + job.Name + "」。", nil
		},
	}}
}

// jobText 是一条任务给模型看的一行描述。
func jobText(j schedule.Job) string {
	loc := j.Location()
	state := "启用"
	if !j.Enabled {
		state = "已停用"
		if j.DisabledReason != "" {
			state += "（" + j.DisabledReason + "）"
		}
	}
	out := fmt.Sprintf("「%s」(id %s) · %s · %s", j.Name, j.ID, j.Describe(), state)
	if j.Enabled && j.NextRunAt != nil {
		out += " · 下次 " + j.NextRunAt.In(loc).Format("2006-01-02 15:04")
	}
	if j.LastRunAt != nil {
		out += fmt.Sprintf(" · 上次 %s %s", j.LastRunAt.In(loc).Format("01-02 15:04"), jobStatusWord(j.LastStatus))
		if j.LastSummary != "" {
			out += "：" + trimRunes(j.LastSummary, 120)
		}
	}
	return out
}

// jobErr 把 schedule 包的哨兵换成本包的（httpapi 只认识本包那套）。
func jobErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, schedule.ErrNotFound):
		return fmt.Errorf("%w: %v", ErrNotFound, strings.TrimPrefix(err.Error(), "schedule: "))
	case errors.Is(err, schedule.ErrInvalid), errors.Is(err, schedule.ErrRunning):
		return fmt.Errorf("%w: %v", ErrInvalid, strings.TrimPrefix(err.Error(), "schedule: "))
	}
	return err
}
