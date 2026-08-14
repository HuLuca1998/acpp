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

func (h systemHandler) env(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, h.system.EnvCheck(r.Context()))
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
