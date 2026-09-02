package httpapi

import (
	"fmt"
	"net"
	"net/http"

	"acpp/server/internal/config"
	"acpp/server/internal/discord"
	"acpp/server/internal/service"
	"acpp/server/internal/system"
	"acpp/server/internal/titler"
)

type systemHandler struct {
	system *system.Service
	// addr 是本服务的监听地址，用来拼局域网可访问的地址前缀
	// （与 tenantHandler 拼邀请链接是同一件事，见 withLanBase）。
	addr   string
	update *system.Updater
	// titler 是会话标题生成服务，设置页改配置后原地热更（不用重启）。
	titler *titler.Service
	// busyTurns 数正在跑的轮。自更新会杀掉全部 agent 子进程，正有人
	// 等回复时必须先提示、拿到明确确认才动手（实测踩过：更新时一条
	// 会话在途，进程被带走，那轮永远等不到响应）。
	busyTurns func() int
}

func (h systemHandler) updateInfo(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "1"
	writeData(w, http.StatusOK, h.update.Info(r.Context(), force))
}

func (h systemHandler) updateApply(w http.ResponseWriter, r *http.Request) {
	// body 可选：老客户端与桌面壳可能裸 POST，等价 force=false。
	var in struct {
		Force bool `json:"force"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, err)
			return
		}
	}
	if busy := h.busyTurns(); busy > 0 && !in.Force {
		// 不是错误而是待确认：前端据此弹「有人在干活」的确认框，
		// 用户点了继续再带 force 重发。
		writeData(w, http.StatusOK, map[string]any{"applied": false, "runningTurns": busy})
		return
	}
	message, err := h.update.Apply(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"applied": true, "message": message})
}

func (h systemHandler) get(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, h.withLanBase(h.system.Info()))
}

// systemInfoView 在系统信息上补一条局域网地址前缀。
//
// 单独包一层而不是塞进 system.SystemInfo：IP 与端口是 http 层才知道的事，
// 业务层不该为了拼一条 URL 反过来认识监听地址。tenantHandler 拼邀请链接
// 用的是同一套做法。
type systemInfoView struct {
	system.SystemInfo
	// LanBase 是局域网里访问本服务的地址前缀（`http://<ip>:<端口>`）。
	// 界面用它把相对路径拼成可以转发给同事的完整链接。
	LanBase string `json:"lanBase"`
	// LanShareable 如实说明这条地址当下能不能真发出去：只监听回环时
	// 给的是 127.0.0.1，那条链接只有本机点得开，界面据此提示用户。
	LanShareable bool `json:"lanShareable"`
}

func (h systemHandler) withLanBase(info system.SystemInfo) systemInfoView {
	host, port, err := net.SplitHostPort(h.addr)
	if err != nil || port == "" {
		port = "48080"
	}
	shareable := listensBeyondLoopback(host)
	target := lanIP()
	if !shareable {
		target = "127.0.0.1"
	}
	return systemInfoView{
		SystemInfo:   info,
		LanBase:      fmt.Sprintf("http://%s:%s", target, port),
		LanShareable: shareable,
	}
}

func (h systemHandler) env(w http.ResponseWriter, r *http.Request) {
	// refresh=1 是「重新检测」按钮：绕过最新版缓存，重打一次 registry。
	refresh := r.URL.Query().Get("refresh") == "1"
	writeData(w, http.StatusOK, h.system.EnvCheck(r.Context(), refresh))
}

func (h systemHandler) envInstall(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	res, err := h.system.EnvInstall(r.Context(), in.Key)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, res)
}

// workspaceDir 改工作区根（立刻生效，不需要重启）。
func (h systemHandler) workspaceDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspaceDir string `json:"workspaceDir"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	info, err := h.system.SetWorkspaceDir(req.WorkspaceDir)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, info)
}

func (h systemHandler) migrate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DataDir string `json:"dataDir"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	info, err := h.system.MigrateDataDir(r.Context(), in.DataDir)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, info)
}

// titleModel 回显当前的标题模型配置。
func (h systemHandler) titleModel(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, h.titler.Config())
}

// saveTitleModel 存配置并热更到运行中的服务。存盘失败就不改内存里的那份，
// 避免「界面显示已保存、重启后又变回去」。
func (h systemHandler) saveTitleModel(w http.ResponseWriter, r *http.Request) {
	var in titler.Config
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	in = in.Normalize()
	if in.Enabled && in.Model == "" {
		writeError(w, fmt.Errorf("%w: 启用标题生成必须选一个模型", service.ErrInvalid))
		return
	}
	if err := config.SaveTitleModel(config.TitleModel{
		Enabled: in.Enabled, BaseURL: in.BaseURL, Model: in.Model,
	}); err != nil {
		writeError(w, err)
		return
	}
	h.titler.Update(in)
	writeData(w, http.StatusOK, h.titler.Config())
}

// titleModels 拉某个 ollama 端点上已装的模型清单。地址走查询参数而不是
// 读配置：设置页要能在「还没保存」的地址上先试一把。
func (h systemHandler) titleModels(w http.ResponseWriter, r *http.Request) {
	models, err := titler.Models(r.Context(), h.titler.Client(), r.URL.Query().Get("baseUrl"))
	if err != nil {
		writeError(w, fmt.Errorf("%w: %s", service.ErrInvalid, err.Error()))
		return
	}
	writeData(w, http.StatusOK, models)
}

// titleModelTest 用当前表单里的配置真生成一个标题，让用户当场看到效果
// 与耗时，而不是保存完去猜。不落盘、不改运行中的配置。
func (h systemHandler) titleModelTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		titler.Config
		User  string `json:"user"`
		Agent string `json:"agent"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	cfg := in.Config.Normalize()
	cfg.Enabled = true
	if cfg.Model == "" {
		writeError(w, fmt.Errorf("%w: 先选一个模型", service.ErrInvalid))
		return
	}
	user := in.User
	if user == "" {
		user = "帮我看看这个"
	}
	agent := in.Agent
	if agent == "" {
		agent = "你贴的报错是 SSE 重连后事件重复消费，根因在 broker 的重放缓冲没按轮次清空。"
	}
	title, err := titler.New(cfg).Generate(r.Context(), user, agent)
	if err != nil {
		writeError(w, fmt.Errorf("%w: %s", service.ErrInvalid, err.Error()))
		return
	}
	writeData(w, http.StatusOK, map[string]string{"title": title})
}

// ---- discord 频道工作区（adr-016）----
// handler 落在 system.go 而不是独立文件：httpapi 贴着反平铺硬线，且
// discord 配置面与 title-model 同属「系统平台配置」一类。

type discordHandler struct {
	discord *discord.Service
}

func (h discordHandler) get(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, h.discord.Info(r.Context()))
}

func (h discordHandler) saveConfig(w http.ResponseWriter, r *http.Request) {
	var in discord.ConfigPatch
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	info, err := h.discord.SaveConfig(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, info)
}

func (h discordHandler) updateBinding(w http.ResponseWriter, r *http.Request) {
	var in discord.BindingPatch
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	binding, err := h.discord.UpdateBinding(r.Context(), r.PathValue("channelId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, binding)
}

func (h discordHandler) removeBinding(w http.ResponseWriter, r *http.Request) {
	if err := h.discord.RemoveBinding(r.PathValue("channelId")); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"deleted": true})
}
