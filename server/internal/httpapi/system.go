package httpapi

import (
	"net/http"

	"acpp/server/internal/service"
)

type systemHandler struct {
	system *service.SystemService
}

func (h systemHandler) get(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, h.system.Info())
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
