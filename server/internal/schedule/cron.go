package schedule

// 本文件是计划的「什么时候」：cron 表达式解析（robfig/cron 的 parser，
// 5 段 + IANA 时区）、下一次时刻、本机时区推断与给人看的描述。调度循环
// 与存储在 schedule.go。

import (
	"fmt"
	"os"
	"strings"
	"time"

	cron "github.com/robfig/cron/v3"
)

// parser 只认 5 段（分 时 日 月 周）与 @daily 这类描述符；秒级粒度对
// 「跑一轮 agent」毫无意义。
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// parseCron 解析表达式；tz 非空时按该时区的墙钟解释（不预转 UTC）。
func parseCron(expr, tz string) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("%w: cron 表达式为空", ErrInvalid)
	}
	if strings.HasPrefix(expr, "TZ=") || strings.HasPrefix(expr, "CRON_TZ=") {
		return nil, fmt.Errorf("%w: 时区请放 tz 字段，不要写进表达式", ErrInvalid)
	}
	spec := expr
	if tz != "" {
		spec = "CRON_TZ=" + tz + " " + expr
	}
	sched, err := parser.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("%w: cron 表达式 %q：%v", ErrInvalid, expr, err)
	}
	return sched, nil
}

// Next 算表达式在 after 之后的下一次时刻（校验也走它：解析不过就报错）。
func Next(expr, tz string, after time.Time) (time.Time, error) {
	sched, err := parseCron(expr, tz)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(after), nil
}

// nextAfter 是任务下一次计划时刻：停用的没有；一次性的没跑过就是 At；
// 周期的按表达式算。表达式解析失败按没有处理（Add/Update 已经拦过）。
func nextAfter(j Job, after time.Time) *time.Time {
	if !j.Enabled {
		return nil
	}
	if j.At != nil {
		if j.LastRunAt == nil {
			return ptr(*j.At)
		}
		return nil
	}
	sched, err := parseCron(j.Cron, j.TZ)
	if err != nil {
		return nil
	}
	n := sched.Next(after)
	if n.IsZero() {
		return nil
	}
	return &n
}

// HostTZ 猜本机的 IANA 时区名（macOS/Linux 的 /etc/localtime 软链）；
// 猜不到返回空——空在本包语义里就是「本机时区」，只是显示不出名字。
func HostTZ() string {
	if tz := os.Getenv("TZ"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	if i := strings.Index(target, "zoneinfo/"); i >= 0 {
		name := target[i+len("zoneinfo/"):]
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}
	return ""
}

// Location 把任务的 TZ 解析成 *time.Location，空或不认识退回本机。
func (j Job) Location() *time.Location {
	if j.TZ != "" {
		if loc, err := time.LoadLocation(j.TZ); err == nil {
			return loc
		}
	}
	return time.Local
}

// Describe 把计划说成人话（常见形状），说不上来就原样给表达式。
func (j Job) Describe() string {
	if j.At != nil {
		return "一次性 · " + j.At.In(j.Location()).Format("01-02 15:04")
	}
	f := strings.Fields(j.Cron)
	desc := j.Cron
	if len(f) == 5 {
		m, h, dom, mon, dow := f[0], f[1], f[2], f[3], f[4]
		hm := func() string {
			if isNum(m) && isNum(h) {
				return fmt.Sprintf("%02s:%02s", h, m)
			}
			return ""
		}()
		switch {
		case hm != "" && dom == "*" && mon == "*" && dow == "*":
			desc = "每天 " + hm
		case hm != "" && dom == "*" && mon == "*" && weekdayName(dow) != "":
			desc = "每" + weekdayName(dow) + " " + hm
		case hm != "" && isNum(dom) && mon == "*" && dow == "*":
			desc = "每月 " + dom + " 日 " + hm
		case isNum(m) && strings.HasPrefix(h, "*/") && dom == "*" && mon == "*" && dow == "*":
			desc = "每 " + strings.TrimPrefix(h, "*/") + " 小时"
		case strings.HasPrefix(m, "*/") && h == "*" && dom == "*" && mon == "*" && dow == "*":
			desc = "每 " + strings.TrimPrefix(m, "*/") + " 分钟"
		}
	}
	if j.TZ != "" {
		desc += " (" + j.TZ + ")"
	}
	return desc
}

func isNum(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func weekdayName(dow string) string {
	names := map[string]string{
		"0": "周日", "7": "周日", "1": "周一", "2": "周二", "3": "周三", "4": "周四", "5": "周五", "6": "周六",
		"1-5": "个工作日", "MON": "周一", "TUE": "周二", "WED": "周三", "THU": "周四", "FRI": "周五", "SAT": "周六", "SUN": "周日",
	}
	return names[strings.ToUpper(dow)]
}
