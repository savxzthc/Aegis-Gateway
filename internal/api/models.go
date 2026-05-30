package api

import (
	"net/http"

	"github.com/savxzthc/aegis-gateway/internal/config"
)

// Models handles GET /v1/models.
func (s *Server) Models(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config.Get()
	names := config.SortedModels(cfg.Models.Registry)
	data := make([]modelObject, 0, len(names))
	for _, name := range names {
		modelCfg := cfg.Models.Registry[name]
		backend := modelCfg.Backend
		if backend == "" {
			backend = cfg.Backend.DefaultType
		}
		data = append(data, modelObject{
			ID:          name,
			Object:      "model",
			Created:     0,
			OwnedBy:     "local",
			VRAMGB:      modelCfg.VRAMGB,
			Backend:     backend,
			Description: modelCfg.Description,
			Status:      string(s.Lifecycle.Status(name)),
		})
	}
	writeJSON(w, http.StatusOK, modelListResponse{Object: "list", Data: data})
}

type modelListResponse struct {
	Object string        `json:"object"`
	Data   []modelObject `json:"data"`
}

type modelObject struct {
	ID          string  `json:"id"`
	Object      string  `json:"object"`
	Created     int64   `json:"created"`
	OwnedBy     string  `json:"owned_by"`
	VRAMGB      float64 `json:"vram_gb"`
	Backend     string  `json:"backend"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
}
