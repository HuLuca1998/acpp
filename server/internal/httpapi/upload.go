package httpapi

import (
	"fmt"
	"net/http"

	"acpp/server/internal/service"
	"acpp/server/internal/upload"
)

// uploadHandler 是本地文件上传面。上传件落在各自身份的家目录下（owner 是
// 工作区根，租户是自己的 root），隔离由路径本身给——不需要再加一层归属
// 过滤，租户连别人的目录都进不去。
type uploadHandler struct{}

// 单个文件的上限。往 prompt 里塞的东西早晚要过 agent 的上下文，几百 MB
// 的文件传上去也没法用；把线画在这里，让用户当场知道，而不是传完了再在
// 发送时报错。
const maxUploadBytes = 32 << 20 // 32 MiB

func (h uploadHandler) list(w http.ResponseWriter, r *http.Request) {
	files, err := upload.ListUploads(scopeOf(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, newPage(files))
}

func (h uploadHandler) create(w http.ResponseWriter, r *http.Request) {
	// MaxBytesReader 兜在最外层：ParseMultipartForm 自己的上限只管内存里
	// 那部分，超出的会落到临时文件，光靠它挡不住一个超大的 body。
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, fmt.Errorf("%w: upload: %s", service.ErrInvalid, err))
		return
	}
	defer file.Close()

	saved, err := upload.SaveUpload(scopeOf(r), header.Filename, file)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, saved)
}

func (h uploadHandler) remove(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if err := upload.DeleteUpload(scopeOf(r), q.Get("hash"), q.Get("name")); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"deleted": true})
}
