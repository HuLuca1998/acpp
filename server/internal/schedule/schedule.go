// Package schedule 是定时任务的调度核心：任务与运行记录的存储、cron 表达式
// 解析、到点触发、同任务不重入、失败退避与自动停用。
//
// 它刻意不知道两件事：任务**怎么跑**（交给调用方注入的 Runner）与任务
// **属于谁**（Scope 是调用方自定义的不透明字符串，discord 用频道 id）。
// 网页会话侧将来要定时，同一个包再接一个 Runner 即可。不 import 本项目
// 其他包。
package schedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid  = errors.New("schedule: invalid")
	ErrNotFound = errors.New("schedule: not found")
	// ErrBusy 由 Runner 返回：资源暂时不可用（会话池满、渠道断线）。
	// 不算失败——这一次尝试从记录里撤掉，一分钟后再试，连续多次仍不可用
	// 才转成失败。
	ErrBusy = errors.New("schedule: runner busy")
	// ErrRunning 表示任务正在运行（手动触发时撞上了）。
	ErrRunning = errors.New("schedule: job is running")
)

// 运行状态与触发来源的取值。
const (
	StatusRunning = "running"
	StatusOK      = "ok"
	StatusSilent  = "silent"
	StatusError   = "error"
	StatusSkipped = "skipped"

	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

const (
	maxRuns   = 30
	maxName   = 80
	maxPrompt = 16 * 1024
	// missedGrace：到点后超过这么久才发现（服务没跑、机器休眠），这一次
	// 不补跑——照 openclaw 的口径「过期任务重新排期而不是立刻补跑」，只是
	// 宽限给得比它松：晚半小时以内仍算准时。
	missedGrace = 30 * time.Minute
	busyRetry   = time.Minute
	busyMax     = 10
	// disableAfter：连续失败这么多次自动停用。比 openclaw 的 10 次保守——
	// 这里一次运行是一整条 agent 会话，比它贵。
	disableAfter = 5
)

// backoffs 是连续失败后的重试间隔（第 1/2/3+ 次）。
var backoffs = []time.Duration{5 * time.Minute, 15 * time.Minute, 60 * time.Minute}

// Job 是一条定时任务。「什么时候、干什么」在这里；「用什么环境跑」由
// Scope 指向的调用方对象决定，这里不存。
type Job struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
	Name  string `json:"name"`
	// Cron 是 5 段表达式，按 TZ 的墙钟解释；TZ 空表示本机时区。
	Cron string `json:"cron,omitempty"`
	TZ   string `json:"tz,omitempty"`
	// At 非空表示一次性任务：跑成功即删。
	At        *time.Time `json:"at,omitempty"`
	Prompt    string     `json:"prompt"`
	Enabled   bool       `json:"enabled"`
	CreatedBy string     `json:"createdBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`

	LastRunAt   *time.Time `json:"lastRunAt,omitempty"`
	LastStatus  string     `json:"lastStatus,omitempty"`
	LastSummary string     `json:"lastSummary,omitempty"`
	FailStreak  int        `json:"failStreak,omitempty"`
	// DisabledReason 非空表示被系统停用（连续失败）；人工启用时清空。
	DisabledReason string `json:"disabledReason,omitempty"`
	// NextRunAt 是下一次计划时刻（含失败重试）。落盘只是缓存，加载时重算。
	NextRunAt *time.Time `json:"nextRunAt,omitempty"`
	// Running 是运行态标记；进程重启后一律归零，末尾那条 running 记录改判中断。
	Running bool `json:"running,omitempty"`
	// Plan 是计划的人话（Describe 的结果），只在对外视图里填，不落盘。
	Plan string `json:"plan,omitempty"`
	Runs []Run  `json:"runs,omitempty"`
}

// Run 是一次运行记录（最近 maxRuns 条）。
type Run struct {
	ID        string     `json:"id"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Status    string     `json:"status"`
	Trigger   string     `json:"trigger"`
	// Ref 是调用方的落点标识（discord：子区 id），点得进去看过程。
	Ref     string `json:"ref,omitempty"`
	Tools   int    `json:"tools,omitempty"`
	Tokens  int    `json:"tokens,omitempty"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Result 是 Runner 跑完一次的回报。Status 只认 ok / silent / error。
type Result struct {
	Status  string
	Ref     string
	Tools   int
	Tokens  int
	Summary string
	Error   string
}

// Runner 跑一次任务。返回 ErrBusy 表示资源暂不可用（稍后重试，不算失败）；
// 其他 error 与 Result.Status=error 一样算失败。
type Runner func(ctx context.Context, job Job, run Run) (Result, error)

// Hooks 是调度器向调用方通报的事件。
type Hooks struct {
	// Disabled 在任务因连续失败被自动停用时调用（异步）。
	Disabled func(job Job, reason string)
}

// Input 是新建任务的入参。
type Input struct {
	Scope     string
	Name      string
	Cron      string
	TZ        string
	At        *time.Time
	Prompt    string
	CreatedBy string
}

// Patch 是改任务的入参：nil 表示不动。ClearAt 把一次性任务改成周期任务时用。
type Patch struct {
	Name    *string
	Cron    *string
	TZ      *string
	At      *time.Time
	ClearAt bool
	Prompt  *string
	Enabled *bool
}

// Service 是调度器：单文件 JSON 存储 + 每分钟一次的扫描。
type Service struct {
	path   string
	runner Runner
	hooks  Hooks
	now    func() time.Time

	mu      sync.Mutex
	jobs    []Job
	running map[string]struct{}
	// retryAt 是失败/忙碌后的下次尝试时刻。内存态：进程重启就从计划重算，
	// 重启本身已经是一次「重新来过」。
	retryAt map[string]time.Time
	busy    map[string]int
	// skipped 记下因上一轮未完成而跳过的那个时刻，同一时刻只记一条。
	skipped map[string]time.Time

	ctx    context.Context
	cancel context.CancelFunc
}

type file struct {
	Jobs []Job `json:"jobs"`
}

// New 加载（或新建）存储。Runner 为 nil 时只能管任务不能跑。
func New(path string, runner Runner, hooks Hooks) (*Service, error) {
	s := &Service{
		path: path, runner: runner, hooks: hooks, now: time.Now,
		running: map[string]struct{}{}, retryAt: map[string]time.Time{},
		busy: map[string]int{}, skipped: map[string]time.Time{},
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("读定时任务: %w", err)
	default:
		var f file
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("解定时任务 %s: %w", path, err)
		}
		s.jobs = f.Jobs
	}
	now := s.now()
	for i := range s.jobs {
		j := &s.jobs[i]
		j.Running = false
		// 上次进程退出时还在跑的那一条，不可能再有结果了。
		if n := len(j.Runs); n > 0 && j.Runs[n-1].Status == StatusRunning {
			r := &j.Runs[n-1]
			r.Status, r.Error = StatusError, "服务重启，运行中断"
			ended := now
			r.EndedAt = &ended
			j.LastStatus = StatusError
		}
		j.NextRunAt = nextAfter(*j, now)
	}
	return s, nil
}

// Start 起扫描循环；ctx 结束即停。
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()
	go s.loop()
}

// Close 停掉扫描并取消正在跑的运行。
func (s *Service) Close() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}

// loop 按整分钟对齐扫描：任务的粒度是分钟，对齐后「10:00」就真的在 10:00。
func (s *Service) loop() {
	for {
		s.mu.Lock()
		ctx := s.ctx
		s.mu.Unlock()
		if ctx == nil {
			return
		}
		next := time.Now().Truncate(time.Minute).Add(time.Minute)
		t := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		s.tick(s.now())
	}
}

// tick 扫一遍：到点的起跑，撞上上一轮未完的记 skipped，错过太久的记
// skipped 并重排。导出给调用方与测试驱动时钟用。
func (s *Service) tick(now time.Time) {
	s.mu.Lock()
	var due []Job
	dirty := false
	for i := range s.jobs {
		j := &s.jobs[i]
		at, ok := s.dueAt(j)
		if !ok || at.After(now) {
			continue
		}
		if _, running := s.running[j.ID]; running {
			if last, seen := s.skipped[j.ID]; !seen || !last.Equal(at) {
				s.skipped[j.ID] = at
				j.addRun(Run{ID: newID("r"), StartedAt: at, EndedAt: &now,
					Status: StatusSkipped, Trigger: TriggerSchedule, Error: "上一轮还没跑完，这一次跳过"})
				// 这一格让过去了：计划推到下一格，否则上一轮一结束它就补跑。
				delete(s.retryAt, j.ID)
				j.NextRunAt = nextAfter(*j, now)
				dirty = true
			}
			continue
		}
		if now.Sub(at) > missedGrace {
			j.addRun(Run{ID: newID("r"), StartedAt: at, EndedAt: &now,
				Status: StatusSkipped, Trigger: TriggerSchedule, Error: "错过执行时间（服务未运行或机器休眠），不补跑"})
			delete(s.retryAt, j.ID)
			if j.At != nil {
				j.Enabled = false
				j.DisabledReason = "错过执行时间，已停用"
			}
			j.NextRunAt = nextAfter(*j, now)
			dirty = true
			continue
		}
		due = append(due, *j)
	}
	if dirty {
		s.write()
	}
	s.mu.Unlock()
	for _, j := range due {
		s.launch(j, TriggerSchedule)
	}
}

// dueAt 给出任务下一次该跑的时刻：失败/忙碌重试优先于计划。
func (s *Service) dueAt(j *Job) (time.Time, bool) {
	if !j.Enabled {
		return time.Time{}, false
	}
	if at, ok := s.retryAt[j.ID]; ok {
		return at, true
	}
	if j.NextRunAt == nil {
		return time.Time{}, false
	}
	return *j.NextRunAt, true
}

// launch 起一次运行：记录先落盘（进程中途死了也知道曾经跑过），再异步跑。
func (s *Service) launch(job Job, trigger string) {
	s.mu.Lock()
	i := s.index(job.ID)
	if i < 0 {
		s.mu.Unlock()
		return
	}
	if _, running := s.running[job.ID]; running {
		s.mu.Unlock()
		return
	}
	if s.runner == nil {
		s.mu.Unlock()
		return
	}
	j := &s.jobs[i]
	run := Run{ID: newID("r"), StartedAt: s.now(), Status: StatusRunning, Trigger: trigger}
	j.addRun(run)
	j.Running = true
	s.running[j.ID] = struct{}{}
	delete(s.retryAt, j.ID)
	if trigger == TriggerSchedule {
		j.NextRunAt = nextAfter(*j, run.StartedAt)
	}
	snapshot := *j
	ctx := s.ctx
	s.write()
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		res, err := s.callRunner(ctx, snapshot, run)
		s.finish(snapshot.ID, run.ID, trigger, res, err)
	}()
}

func (s *Service) callRunner(ctx context.Context, job Job, run Run) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("runner panic: %v", r)
		}
	}()
	return s.runner(ctx, job, run)
}

// finish 收一次运行的结果：更新记录、连败计数、重试与自动停用。
func (s *Service) finish(jobID, runID, trigger string, res Result, err error) {
	s.mu.Lock()
	delete(s.running, jobID)
	i := s.index(jobID)
	if i < 0 {
		// 跑的途中任务被删了：结果无处可记。
		s.mu.Unlock()
		return
	}
	j := &s.jobs[i]
	j.Running = false
	ended := s.now()

	if errors.Is(err, ErrBusy) {
		s.busy[jobID]++
		if s.busy[jobID] <= busyMax {
			j.dropRun(runID)
			if trigger == TriggerSchedule {
				s.retryAt[jobID] = ended.Add(busyRetry)
				j.NextRunAt = ptr(s.retryAt[jobID])
			}
			s.write()
			s.mu.Unlock()
			return
		}
		err = fmt.Errorf("重试 %d 次仍不可用：%w", busyMax, err)
	}
	delete(s.busy, jobID)

	r := j.run(runID)
	if r == nil {
		s.mu.Unlock()
		return
	}
	r.EndedAt = &ended
	r.Ref, r.Tools, r.Tokens, r.Summary = res.Ref, res.Tools, res.Tokens, res.Summary
	switch {
	case err != nil:
		r.Status, r.Error = StatusError, err.Error()
	case res.Status == StatusError:
		r.Status, r.Error = StatusError, res.Error
	case res.Status == StatusSilent:
		r.Status = StatusSilent
	default:
		r.Status = StatusOK
	}
	started := r.StartedAt
	j.LastRunAt = &started
	j.LastStatus = r.Status
	j.LastSummary = r.Summary
	if r.Status == StatusError {
		j.LastSummary = r.Error
	}

	var disabled string
	if r.Status == StatusError {
		j.FailStreak++
		if trigger == TriggerSchedule {
			if j.FailStreak >= disableAfter {
				disabled = fmt.Sprintf("连续失败 %d 次，已自动停用", j.FailStreak)
				j.Enabled, j.DisabledReason, j.NextRunAt = false, disabled, nil
			} else {
				at := ended.Add(backoffs[min(j.FailStreak-1, len(backoffs)-1)])
				s.retryAt[jobID] = at
				j.NextRunAt = &at
			}
		}
	} else {
		j.FailStreak = 0
		if j.At != nil {
			// 一次性任务跑成功即删——它的使命完成了。
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
		}
	}
	snapshot := *j
	s.write()
	s.mu.Unlock()
	if disabled != "" && s.hooks.Disabled != nil {
		go s.hooks.Disabled(snapshot, disabled)
	}
}

// ---- 管理面 ----

// Jobs 列任务；scope 为空列全部。返回副本，调用方随便改。
func (s *Service) Jobs(scope string) []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		if scope == "" || j.Scope == scope {
			out = append(out, s.view(j))
		}
	}
	return out
}

// Get 按 id 取任务。
func (s *Service) Get(id string) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.index(id); i >= 0 {
		return s.view(s.jobs[i]), true
	}
	return Job{}, false
}

// view 给出对外的任务视图：下次时刻含重试、运行记录拷贝。
func (s *Service) view(j Job) Job {
	out := j
	out.Runs = append([]Run(nil), j.Runs...)
	out.Plan = j.Describe()
	if at, ok := s.retryAt[j.ID]; ok {
		out.NextRunAt = ptr(at)
	}
	_, out.Running = s.running[j.ID]
	return out
}

// Add 新建任务。
func (s *Service) Add(in Input) (Job, error) {
	now := s.now()
	j := Job{
		ID: newID("j"), Scope: strings.TrimSpace(in.Scope), Name: strings.TrimSpace(in.Name),
		Cron: strings.TrimSpace(in.Cron), TZ: strings.TrimSpace(in.TZ), At: in.At,
		Prompt: strings.TrimSpace(in.Prompt), Enabled: true, CreatedBy: in.CreatedBy,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := validate(j, now); err != nil {
		return Job{}, err
	}
	j.NextRunAt = nextAfter(j, now)
	s.mu.Lock()
	s.jobs = append(s.jobs, j)
	s.write()
	out := s.view(j)
	s.mu.Unlock()
	return out, nil
}

// Update 改任务：只动给了的字段；启用时清掉自动停用的原因与连败计数。
func (s *Service) Update(id string, p Patch) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return Job{}, ErrNotFound
	}
	next := s.jobs[i]
	if p.Name != nil {
		next.Name = strings.TrimSpace(*p.Name)
	}
	if p.Cron != nil {
		next.Cron = strings.TrimSpace(*p.Cron)
		if next.Cron != "" {
			next.At = nil
		}
	}
	if p.TZ != nil {
		next.TZ = strings.TrimSpace(*p.TZ)
	}
	if p.ClearAt {
		next.At = nil
	}
	if p.At != nil {
		next.At = p.At
		next.Cron = ""
	}
	if p.Prompt != nil {
		next.Prompt = strings.TrimSpace(*p.Prompt)
	}
	if p.Enabled != nil {
		next.Enabled = *p.Enabled
		if *p.Enabled {
			next.DisabledReason = ""
			next.FailStreak = 0
			delete(s.retryAt, id)
			delete(s.busy, id)
		}
	}
	now := s.now()
	if err := validate(next, now); err != nil {
		return Job{}, err
	}
	next.UpdatedAt = now
	next.NextRunAt = nextAfter(next, now)
	// 计划改了，之前挂着的重试就不作数了。
	if p.Cron != nil || p.At != nil || p.TZ != nil {
		delete(s.retryAt, id)
	}
	s.jobs[i] = next
	s.write()
	return s.view(next), nil
}

// Remove 删任务。正在跑的那一次照跑完，结果无处可记而已。
func (s *Service) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return ErrNotFound
	}
	s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
	delete(s.retryAt, id)
	delete(s.busy, id)
	s.write()
	return nil
}

// RemoveScope 删掉一个归属下的全部任务（频道解绑时用），返回删了几条。
func (s *Service) RemoveScope(scope string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.jobs[:0]
	n := 0
	for _, j := range s.jobs {
		if j.Scope == scope {
			n++
			delete(s.retryAt, j.ID)
			delete(s.busy, j.ID)
			continue
		}
		kept = append(kept, j)
	}
	s.jobs = kept
	if n > 0 {
		s.write()
	}
	return n
}

// RunNow 立刻跑一次（不看启用状态——停用的任务也该能手动试）。
func (s *Service) RunNow(id string) error {
	s.mu.Lock()
	i := s.index(id)
	if i < 0 {
		s.mu.Unlock()
		return ErrNotFound
	}
	if _, running := s.running[id]; running {
		s.mu.Unlock()
		return ErrRunning
	}
	if s.runner == nil {
		s.mu.Unlock()
		return fmt.Errorf("%w: 没有配置 runner", ErrInvalid)
	}
	job := s.jobs[i]
	s.mu.Unlock()
	s.launch(job, TriggerManual)
	return nil
}

// ---- 内部 ----

func (s *Service) index(id string) int {
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			return i
		}
	}
	return -1
}

// write 原子落盘（临时文件 + rename）。锁内调用；失败只记日志——内存态
// 已经更新，下一次写盘会把它带上。
func (s *Service) write() {
	jobs := make([]Job, len(s.jobs))
	for i, j := range s.jobs {
		j.Plan = ""
		jobs[i] = j
	}
	data, err := json.MarshalIndent(file{Jobs: jobs}, "", "  ")
	if err != nil {
		slog.Error("定时任务序列化失败", "err", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		slog.Error("建定时任务目录失败", "err", err)
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Error("写定时任务失败", "err", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		slog.Error("落盘定时任务失败", "err", err)
	}
}

func (j *Job) addRun(r Run) {
	j.Runs = append(j.Runs, r)
	if len(j.Runs) > maxRuns {
		j.Runs = append([]Run(nil), j.Runs[len(j.Runs)-maxRuns:]...)
	}
}

func (j *Job) run(id string) *Run {
	for i := range j.Runs {
		if j.Runs[i].ID == id {
			return &j.Runs[i]
		}
	}
	return nil
}

func (j *Job) dropRun(id string) {
	for i := range j.Runs {
		if j.Runs[i].ID == id {
			j.Runs = append(j.Runs[:i], j.Runs[i+1:]...)
			return
		}
	}
}

// validate 是任务定义的唯一校验点：Add 与 Update 共用。
func validate(j Job, now time.Time) error {
	switch {
	case j.Scope == "":
		return fmt.Errorf("%w: scope 为空", ErrInvalid)
	case j.Name == "":
		return fmt.Errorf("%w: 任务要有名字", ErrInvalid)
	case len([]rune(j.Name)) > maxName:
		return fmt.Errorf("%w: 名字超过 %d 字", ErrInvalid, maxName)
	case j.Prompt == "":
		return fmt.Errorf("%w: 任务提示词为空", ErrInvalid)
	case len(j.Prompt) > maxPrompt:
		return fmt.Errorf("%w: 任务提示词超过 %dKB", ErrInvalid, maxPrompt/1024)
	case j.Cron == "" && j.At == nil:
		return fmt.Errorf("%w: 要么给 cron 表达式，要么给一次性时刻 at", ErrInvalid)
	case j.Cron != "" && j.At != nil:
		return fmt.Errorf("%w: cron 与 at 只能二选一", ErrInvalid)
	}
	if j.TZ != "" {
		if _, err := time.LoadLocation(j.TZ); err != nil {
			return fmt.Errorf("%w: 时区 %q 不认识（要 IANA 名字，如 Asia/Shanghai）", ErrInvalid, j.TZ)
		}
	}
	if j.Cron != "" {
		if _, err := parseCron(j.Cron, j.TZ); err != nil {
			return err
		}
	}
	if j.At != nil && j.Enabled && j.LastRunAt == nil && j.At.Before(now.Add(-missedGrace)) {
		return fmt.Errorf("%w: 一次性时刻已经过去了", ErrInvalid)
	}
	return nil
}

func newID(prefix string) string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "_" + fmt.Sprint(time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

func ptr(t time.Time) *time.Time { return &t }
