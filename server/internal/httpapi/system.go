package httpapi

import (
	"net/http"

	"acpp/server/internal/system"
)

type systemHandler struct {
	system *system.Service
	update *system.Updater
}

func (h systemHandler) updateInfo(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "1"
	writeData(w, http.StatusOK, h.update.Info(r.Context(), force))
}

func (h systemHandler) updateApply(w http.ResponseWriter, r *http.Request) {
	message, err := h.update.Apply(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"message": message})
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
