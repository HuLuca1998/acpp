package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// 本文件是 /init 三张选择卡（基础分支 / 数据库 / 服务器）的卡片渲染与
// 「刷新清单」按钮。渲染独立成函数，是因为刷新必须画出一模一样的卡——
// 拉取处与刷新处各画一遍，迟早画歪。拉取与下一步流程在 init.go，
// 交互分发在 interaction.go。

// 刷新按钮的 custom_id 前缀。与下拉的前缀（br: / db: / srv:）刻意不构成
// 前缀关系——按钮和下拉都是 type 3 的组件交互，分发只能靠 HasPrefix 分，
// "brR:" 不以 "br:" 开头才不会被下拉那条抢走。
const (
	pickBranchRefresh = "brR"
	pickDBRefresh     = "dbR"
	pickServerRefresh = "srvR"
)

// pickRows 把下拉和它的刷新按钮拼成组件行。**String Select 独占一个
// action row**（平台限制），按钮塞不进下拉那一行，只能落在下面一行。
func pickRows(sel map[string]any, refreshPrefix, id string) []map[string]any {
	return []map[string]any{
		{"type": 1, "components": []map[string]any{sel}},
		{"type": 1, "components": []map[string]any{{
			"type":      2, // button
			"style":     2, // secondary：它是补救手段，不该抢主选择的注意力
			"label":     "刷新清单",
			"custom_id": refreshPrefix + ":" + id,
			"emoji":     map[string]any{"name": "🔄"},
		}}},
	}
}

// refreshedNote 是刷新后缀在描述末尾的一行。清单前后常常一模一样，
// 没有这行用户看不出按钮到底生效没有。
func refreshedNote(refreshed bool, n int) string {
	if !refreshed {
		return ""
	}
	return fmt.Sprintf("\n🔄 刚刷新 · 当前 %d 项", n)
}

// branchCard 画基础分支选择卡。id 是 pending 记录的键，刷新沿用同一个
// ——刷新不产生新卡，只是把这一张重画。
func branchCard(in initInput, id, defaultBranch string, branches []string, refreshed bool) map[string]any {
	opts := []choice{{Label: defaultBranch + "（默认）", Value: defaultBranch}}
	for _, b := range branches {
		if b == defaultBranch {
			continue
		}
		opts = append(opts, choice{Label: b, Value: b})
		if len(opts) == 25 {
			break
		}
	}
	sel := selectComponent("br:"+id, opts, false)
	sel["type"] = 3 // 消息上的下拉只有 String Select，radio 是 modal 的东西
	sel["placeholder"] = "选择基础分支…"
	delete(sel, "required")

	return map[string]any{
		"embeds": []map[string]any{{
			"title": "选择基础分支",
			"description": fmt.Sprintf(
				"**%s** 有 %d 个分支。频道会从选中的这条切一条自己的工作分支——直接在 pre/prod 上干活提交推不上去。\n15 分钟内有效，过期请重新 /init。%s",
				in.repo, len(branches), refreshedNote(refreshed, len(branches))),
			"color": colorBlurbe,
		}},
		"components":       pickRows(sel, pickBranchRefresh, id),
		"allowed_mentions": noMentions(),
	}
}

// dbCard 画数据库选择卡。
func dbCard(in initInput, id string, dbs []DBOption, currentID uint, refreshed bool) map[string]any {
	sel := selectComponent("db:"+id, dbChoices(dbs, currentID), false)
	sel["type"] = 3 // 消息上的下拉只有 String Select
	sel["placeholder"] = "选择这个频道能查的库…"
	delete(sel, "required")

	branch := in.branch
	if branch == "" {
		branch = in.defaultBranch + "（默认）"
	}
	return map[string]any{
		"embeds": []map[string]any{{
			"title": "选择数据库",
			"description": fmt.Sprintf(
				"**%s**（基于 %s）\n选中之后，这个频道的 AI 只看得见这一条连接——同项目别的环境列都列不出来。\n15 分钟内有效。%s",
				in.repo, branch, refreshedNote(refreshed, len(dbs))),
			"color": colorBlurbe,
		}},
		"components":       pickRows(sel, pickDBRefresh, id),
		"allowed_mentions": noMentions(),
	}
}

// serverCard 画服务器选择卡。
func serverCard(in initInput, id string, hosts []ServerOption, currentID uint, refreshed bool) map[string]any {
	sel := selectComponent("srv:"+id, serverChoices(hosts, currentID), false)
	sel["type"] = 3
	sel["placeholder"] = "选择这个频道能看的机器…"
	delete(sel, "required")

	dbLabel := in.dbRef
	if in.dbID == 0 {
		dbLabel = "不锁定"
	}
	return map[string]any{
		"embeds": []map[string]any{{
			"title": "选择服务器",
			"description": fmt.Sprintf(
				"**%s** · 数据库 **%s**\n选中之后，这个频道的 AI 只看得见这一台机器——别的机器连列都列不出来。\n15 分钟内有效。%s",
				in.repo, dbLabel, refreshedNote(refreshed, len(hosts))),
			"color": colorBlurbe,
		}},
		"components":       pickRows(sel, pickServerRefresh, id),
		"allowed_mentions": noMentions(),
	}
}

// refreshPick 重新拉一次清单并原地重画卡片。三张卡共用这一条路径——
// 它们的差别只在「拉什么」，过期处理与重画方式完全一样。
// **pending 不消耗**：刷新不是选择，卡还是那张卡，用户接着选。
func (s *Service) refreshPick(ctx context.Context, token string, ev interactionEvent) {
	kind, id, _ := strings.Cut(ev.Data.CustomID, ":")
	s.mu.Lock()
	p, ok := s.pending[id]
	s.mu.Unlock()
	if !ok {
		s.ephemeral(token, ev, "这张卡过期了（或已处理过），重新 /init 一次。")
		return
	}

	// 先 ACK 占住 3 秒线：重查要跑 ls-remote 或查库，比这条线慢得多。
	// type 6 是「稍后编辑这条消息」，界面上不会闪成加载态。
	if err := interactionCallback(token, ev.ID, ev.Token, 6, nil); err != nil {
		slog.Error("刷新清单 ACK 失败", "kind", kind, "err", err)
		return
	}

	current, _ := s.store.config().binding(ev.ChannelID)
	var body map[string]any
	switch kind {
	case pickBranchRefresh:
		lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defaultBranch, branches, err := lsRemoteBranches(lctx, p.in.cloneURL)
		cancel()
		if err != nil {
			s.pickRefreshFailed(token, ev, "分支", err)
			return
		}
		// 默认分支可能随远端变了，跟着更新——它决定「选了默认」怎么归一。
		in := p.in
		in.defaultBranch = defaultBranch
		s.updatePendingInput(id, in)
		body = branchCard(in, id, defaultBranch, branches, true)

	case pickDBRefresh:
		if s.deps.DataSources == nil {
			s.pickRefreshFailed(token, ev, "数据库", errNoDeps)
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		dbs, err := s.deps.DataSources(cctx)
		cancel()
		if err != nil {
			s.pickRefreshFailed(token, ev, "数据库", err)
			return
		}
		body = dbCard(p.in, id, dbs, current.DataSourceID, true)

	case pickServerRefresh:
		if s.deps.Servers == nil {
			s.pickRefreshFailed(token, ev, "服务器", errNoDeps)
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		hosts, err := s.deps.Servers(cctx)
		cancel()
		if err != nil {
			s.pickRefreshFailed(token, ev, "服务器", err)
			return
		}
		body = serverCard(p.in, id, hosts, current.ServerID, true)

	default:
		return
	}
	s.editOriginal(token, s.appID(), ev.Token, body)
}

// errNoDeps 是清单来源压根没接上——装配层没注入，不是拉取失败。
var errNoDeps = fmt.Errorf("清单来源未接入")

// updatePendingInput 把重查得到的新信息写回 pending。记录可能刚被选择
// 消耗掉，所以先确认还在。
func (s *Service) updatePendingInput(id string, in initInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.pending[id]; ok {
		cur.in = in
		s.pending[id] = cur
	}
}

// pickRefreshFailed 报刷新失败。此时已经 ACK 过，不能再走 callback，
// 只能补一条 followup；原卡片原样留着，用户照旧能按上一次的清单选。
func (s *Service) pickRefreshFailed(token string, ev interactionEvent, what string, err error) {
	slog.Warn("刷新清单失败", "what", what, "err", err)
	if ferr := followupEphemeral(token, s.appID(), ev.Token, fmt.Sprintf(
		"刷新%s清单失败：%v\n卡片里还是上一次的清单，可以照旧选。", what, err)); ferr != nil {
		slog.Error("刷新失败的提示也没发出去", "err", ferr)
	}
}
