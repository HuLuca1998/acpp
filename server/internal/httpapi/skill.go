package httpapi

import (
	"cmp"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"acpp/server/internal/service"
)

// maxSkillImportBytes 是导入包上传的上限（压缩态）。解开后的总量另有上限，
// 在 service 那边——两处都要挡：小包也能解出几个 G。
const maxSkillImportBytes = 64 << 20

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
	// 筛选也在内存里做，且必须在切页之前——和排序同一个道理。
	skills = filterSkills(skills, r.URL.Query().Get("q"), queryBool(r, "enabled"))
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

// export 下发 zip：路径带 name 就是那一个技能，不带就是整个技能库（换设备
// 搬家用）。
func (h skillHandler) export(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	filename := "acpp-skills.zip"
	if name != "" {
		filename = name + ".zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	setAttachment(w, filename)
	if err := h.skills.ExportZip(name, w); err != nil {
		// 开始写 body 之后状态码就改不了了，只能落日志——客户端拿到的是一个
		// 不完整的 zip。头还没发时正常报错。
		writeError(w, err)
		slog.Warn("export skills", "skill", name, "err", err)
	}
}

// importZip 从上传的 zip 还原技能。导入的技能一律停用，由用户确认后再开。
func (h skillHandler) importZip(w http.ResponseWriter, r *http.Request) {
	// MaxBytesReader 兜在最外层：ParseMultipartForm 的上限只管内存里那部分，
	// 超出的会落到临时文件，光靠它挡不住一个超大的 body。
	r.Body = http.MaxBytesReader(w, r.Body, maxSkillImportBytes)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, fmt.Errorf("%w: upload: %s", service.ErrInvalid, err))
		return
	}
	defer file.Close()

	// zip 要随机读：multipart.File 在小文件时是内存缓冲、大文件时是临时文件，
	// 两种都能 Seek，所以用它量出大小再从头读。
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		writeError(w, fmt.Errorf("%w: upload is not seekable: %s", service.ErrInvalid, err))
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, fmt.Errorf("%w: upload is not seekable: %s", service.ErrInvalid, err))
		return
	}

	res, err := h.skills.ImportZip(file, size)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, res)
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

// filterSkills 按关键词（名字或描述含它，不分大小写）与启用状态过滤。
func filterSkills(skills []service.Skill, keyword string, enabled *bool) []service.Skill {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" && enabled == nil {
		return skills
	}
	out := make([]service.Skill, 0, len(skills))
	for _, sk := range skills {
		if enabled != nil && sk.Enabled != *enabled {
			continue
		}
		if kw != "" &&
			!strings.Contains(strings.ToLower(sk.Name), kw) &&
			!strings.Contains(strings.ToLower(sk.Description), kw) {
			continue
		}
		out = append(out, sk)
	}
	return out
}
