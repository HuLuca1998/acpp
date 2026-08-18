package httpapi

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"acpp/server/internal/service"
)

type skillHandler struct {
	skills *service.SkillService
	usage  *service.SkillUsageService
}

func (h skillHandler) list(w http.ResponseWriter, r *http.Request) {
	skills, err := h.skills.List()
	if err != nil {
		writeError(w, err)
		return
	}
	// 把使用次数合进列表，省一次往返；统计失败不影响列表本身。
	if counts, err := h.usage.CountsByName(r.Context()); err == nil {
		for i := range skills {
			skills[i].UsageCount = counts[skills[i].Name]
		}
	}
	// 技能是扫盘得来的（磁盘即事实源），没有 SQL 可以 ORDER BY / LIMIT——
	// 排序和切页都在内存里做。目录读取本身是 O(n)，但那部分快得多，真正会
	// 拖慢页面的是把几百条连同正文一起塞进一次响应。
	//
	// 顺序必须是「先排后切」：反过来就只排了当前这一页，用户以为看到的是
	// 「全部里用得最多的」，其实是「这 20 条里用得最多的」。
	sortSkills(skills, sortParams(r, "name", "enabled", "updated_at", "usage_count"))

	pageNum, pageSize := pageParams(r)
	total := len(skills)
	writeData(w, http.StatusOK, page[service.Skill]{
		Items:    slicePage(skills, pageNum, pageSize),
		Total:    int64(total),
		Page:     pageNum,
		PageSize: pageSize,
	})
}

// usageTop 返回使用最多的技能，供概览页统计。
func (h skillHandler) usageTop(w http.ResponseWriter, r *http.Request) {
	top, err := h.usage.Top(r.Context(), 10)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(top))
}

func (h skillHandler) get(w http.ResponseWriter, r *http.Request) {
	skill, err := h.skills.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, skill)
}

func (h skillHandler) create(w http.ResponseWriter, r *http.Request) {
	var in service.SkillCreateInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	skill, err := h.skills.Create(in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, skill)
}

func (h skillHandler) update(w http.ResponseWriter, r *http.Request) {
	var in service.SkillUpdateInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	skill, err := h.skills.Update(r.PathValue("name"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, skill)
}

func (h skillHandler) remove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.skills.Delete(name); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---- 附属文件（references/ scripts/ assets/ 等）----

func (h skillHandler) listFiles(w http.ResponseWriter, r *http.Request) {
	files, err := h.skills.ListFiles(r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(files))
}

func (h skillHandler) getFile(w http.ResponseWriter, r *http.Request) {
	file, err := h.skills.GetFile(r.PathValue("name"), r.PathValue("path"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, file)
}

func (h skillHandler) putFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	file, err := h.skills.PutFile(r.PathValue("name"), r.PathValue("path"), in.Content)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, file)
}

// ---- 脚本（scripts/）元信息与试运行 ----

func (h skillHandler) listScripts(w http.ResponseWriter, r *http.Request) {
	scripts, err := h.skills.ListScripts(r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(scripts))
}

func (h skillHandler) runScript(w http.ResponseWriter, r *http.Request) {
	var in service.SkillScriptRunInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}

	result, err := h.skills.RunScript(r.Context(), r.PathValue("name"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

func (h skillHandler) removeFile(w http.ResponseWriter, r *http.Request) {
	if err := h.skills.DeleteFile(r.PathValue("name"), r.PathValue("path")); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"deleted": true})
}

// sortSkills 按请求的字段就地排序。字段名与其他列表端点保持同一套写法
// （snake_case），前端不必知道哪个端点背后是数据库、哪个是磁盘。
func sortSkills(skills []service.Skill, spec SortSpec) {
	if spec.Column == "" {
		// 没指定就用 List 给的顺序（按名字），不做多余的重排。
		return
	}
	compare := skillComparator(spec.Column)
	slices.SortStableFunc(skills, func(a, b service.Skill) int {
		if spec.Desc {
			return compare(b, a)
		}
		return compare(a, b)
	})
}

func skillComparator(column string) func(a, b service.Skill) int {
	switch column {
	case "enabled":
		return func(a, b service.Skill) int {
			return cmp.Compare(boolRank(a.Enabled), boolRank(b.Enabled))
		}
	case "updated_at":
		return func(a, b service.Skill) int { return a.UpdatedAt.Compare(b.UpdatedAt) }
	case "usage_count":
		return func(a, b service.Skill) int {
			return cmp.Compare(a.UsageCount, b.UsageCount)
		}
	default:
		// 名字大小写不敏感：技能名多是小写，混进一个大写开头的会排到最前，
		// 看起来像乱序。
		return func(a, b service.Skill) int {
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		}
	}
}

func boolRank(v bool) int {
	if v {
		return 1
	}
	return 0
}
