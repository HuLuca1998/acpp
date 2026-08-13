package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// SkillUsageService 记录技能被 AI 调用的次数。信号来自 tool_call 事件，
// 两端形状不同（claude 是 Skill 工具的 rawInput.skill / rawOutput 里的
// commandName，codex 是 read SKILL.md），识别逻辑收在 skillNameFromToolCall。
type SkillUsageService struct {
	db     *gorm.DB
	srcDir string

	// seen 按 toolCallId 去重：一次技能触发会产生 tool_call + 多个
	// tool_call_update（同 id），只计一次。进程级即可——tool call 不跨重启。
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewSkillUsageService(db *gorm.DB, dataDir string) *SkillUsageService {
	return &SkillUsageService{
		db:     db,
		srcDir: filepath.Join(dataDir, "skills"),
		seen:   make(map[string]struct{}),
	}
}

// Observe 消费一个归一化事件；是技能调用且该 toolCallId 未计过就计一次。
// 幂等、非阻塞，供 chat 事件流直接调用。
func (s *SkillUsageService) Observe(ev acp.Event) {
	if ev.Kind != acp.EventToolCall || ev.ToolCallID == "" {
		return
	}
	name := skillNameFromToolCall(s.srcDir, ev)
	if name == "" {
		return
	}
	s.mu.Lock()
	if _, ok := s.seen[ev.ToolCallID]; ok {
		s.mu.Unlock()
		return
	}
	s.seen[ev.ToolCallID] = struct{}{}
	s.mu.Unlock()

	usage := model.SkillUsage{Name: name, Count: 1, LastUsedAt: time.Now()}
	// upsert：存在则 count+1、刷新时间。
	if err := s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}},
		DoUpdates: clause.Assignments(map[string]any{
			"count":        gorm.Expr("count + 1"),
			"last_used_at": usage.LastUsedAt,
		}),
	}).Create(&usage).Error; err != nil {
		// 计数是尽力而为的观测数据，失败不该影响对话，记日志即可。
		slog.Warn("skill usage upsert", "skill", name, "err", err)
	}
}

// Delete 清掉一个技能的使用计数，随技能删除一起调用——否则概览的技能
// 使用卡片会显示已不存在的技能、点进去 404。进程内的 seen 不用清：
// 它按一次性的 toolCallId 去重，同名新技能的调用自带新 id，不受影响。
func (s *SkillUsageService) Delete(name string) error {
	if err := s.db.Where("name = ?", name).Delete(&model.SkillUsage{}).Error; err != nil {
		return fmt.Errorf("delete skill usage %s: %w", name, err)
	}
	return nil
}

// Top 返回使用最多的技能，count 降序。
func (s *SkillUsageService) Top(ctx context.Context, limit int) ([]model.SkillUsage, error) {
	if limit <= 0 {
		limit = 10
	}
	var rows []model.SkillUsage
	err := s.db.WithContext(ctx).Order("count desc, last_used_at desc").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("top skill usage: %w", err)
	}
	return rows, nil
}

// CountsByName 返回全部技能的次数，供列表页把 usageCount 合进技能条目。
func (s *SkillUsageService) CountsByName(ctx context.Context) (map[string]int64, error) {
	var rows []model.SkillUsage
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list skill usage: %w", err)
	}
	counts := make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.Name] = r.Count
	}
	return counts, nil
}

// skillNameFromToolCall 从一个 tool_call 事件识别被调用的技能名，不是技能
// 调用返回 ""。两端信号形状不同（详见记忆 skill-call-signals）：
//   - claude：Skill 工具，名字在 rawInput.skill 或 rawOutput.response.commandName，
//     带 plugin 前缀 acpp:，去前缀归一到目录名；
//   - codex：read 工具读 <srcDir>/<name>/SKILL.md，从路径取目录名。
func skillNameFromToolCall(srcDir string, ev acp.Event) string {
	if len(ev.RawInput) > 0 {
		var ri struct {
			Skill string `json:"skill"`
		}
		if json.Unmarshal(ev.RawInput, &ri) == nil && ri.Skill != "" {
			return stripPluginPrefix(ri.Skill)
		}
	}
	if len(ev.RawOutput) > 0 {
		var ro struct {
			Response struct {
				CommandName string `json:"commandName"`
			} `json:"response"`
		}
		if json.Unmarshal(ev.RawOutput, &ro) == nil && ro.Response.CommandName != "" {
			return stripPluginPrefix(ro.Response.CommandName)
		}
	}
	if ev.ToolKind == "read" && len(ev.Locations) > 0 {
		var locs []struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(ev.Locations, &locs) == nil {
			for _, l := range locs {
				if name := skillNameFromPath(srcDir, l.Path); name != "" {
					return name
				}
			}
		}
	}
	return ""
}

// stripPluginPrefix 去掉 "acpp:" 这类 plugin 前缀，归一到技能目录名。
func stripPluginPrefix(s string) string {
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// skillNameFromPath 判断一个被读文件是否某技能的 SKILL.md，是则返回技能
// 目录名。以「文件名是 SKILL.md 且父目录是 srcDir 下真实存在的技能」为准，
// 兼容 codex 读到的真实路径与经符号链接解析的路径。
func skillNameFromPath(srcDir, path string) string {
	if filepath.Base(path) != "SKILL.md" {
		return ""
	}
	name := filepath.Base(filepath.Dir(path))
	if name == "" || name == "." {
		return ""
	}
	if _, err := os.Stat(filepath.Join(srcDir, name, "SKILL.md")); err != nil {
		return ""
	}
	return name
}
