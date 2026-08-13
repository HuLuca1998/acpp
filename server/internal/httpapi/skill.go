package httpapi

import (
	"net/http"

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
	writeData(w, http.StatusOK, newPage(skills))
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
	if err := h.skills.Delete(r.PathValue("name")); err != nil {
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
