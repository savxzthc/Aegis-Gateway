package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/lifecycle"
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

// ModelCatalog handles GET /v1/models/catalog.
func (s *Server) ModelCatalog(w http.ResponseWriter, r *http.Request) {
	online := ollamaLibraryReachable(r.Context())
	cfg := s.Config.Get()
	jobs := pullJobMap(s.Lifecycle.PullJobs())
	installedModels := map[string]bool{}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	installedModels = s.Lifecycle.OllamaModels(ctx)
	cancel()
	data := make([]catalogModelObject, 0, len(downloadCatalog))
	for _, item := range downloadCatalog {
		job, hasJob := jobs[item.ID]
		installed := installedModels[item.ID]
		_, registered := cfg.Models.Registry[item.ID]
		status := "available"
		progress := 0
		message := ""
		if installed {
			status = "installed"
			progress = 100
		}
		if hasJob {
			status = job.Status
			progress = job.ProgressPct
			message = job.Message
		}
		data = append(data, catalogModelObject{
			ID:          item.ID,
			DisplayName: item.DisplayName,
			Backend:     item.Backend,
			VRAMGB:      item.VRAMGB,
			SizeGB:      item.SizeGB,
			Description: item.Description,
			UseCase:     item.UseCase,
			LibraryURL:  item.LibraryURL,
			Installed:   installed,
			Registered:  registered,
			Status:      status,
			ProgressPct: progress,
			Message:     message,
		})
	}
	sort.Slice(data, func(i, j int) bool {
		if data[i].Installed != data[j].Installed {
			return !data[i].Installed
		}
		return data[i].VRAMGB < data[j].VRAMGB
	})
	writeJSON(w, http.StatusOK, catalogResponse{Online: online, Data: data})
}

// PullModel handles POST /v1/models/pull.
func (s *Server) PullModel(w http.ResponseWriter, r *http.Request) {
	var req pullModelRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body", "INVALID_JSON")
		return
	}
	item, ok := catalogByID(req.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, "model is not in the downloadable catalog", "MODEL_NOT_IN_CATALOG")
		return
	}
	if item.Backend != "ollama" {
		writeError(w, http.StatusBadRequest, "only Ollama models can be downloaded", "MODEL_DOWNLOAD_UNSUPPORTED")
		return
	}
	if !ollamaLibraryReachable(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "model library is not reachable", "MODEL_LIBRARY_OFFLINE")
		return
	}
	if _, err := s.Config.RegisterModel(item.ID, config.ModelConfig{
		VRAMGB:      item.VRAMGB,
		Backend:     item.Backend,
		Description: item.Description,
	}); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "model registration failed", "MODEL_REGISTER_FAILED", err)
		return
	}
	job, err := s.Lifecycle.PullModel(r.Context(), item.ID)
	if err != nil {
		writePrivateError(w, r, http.StatusBadGateway, "model download failed to start", "MODEL_PULL_START_FAILED", err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
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

type catalogResponse struct {
	Online bool                 `json:"online"`
	Data   []catalogModelObject `json:"data"`
}

type catalogModelObject struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Backend     string  `json:"backend"`
	VRAMGB      float64 `json:"vram_gb"`
	SizeGB      float64 `json:"size_gb"`
	Description string  `json:"description"`
	UseCase     string  `json:"use_case"`
	LibraryURL  string  `json:"library_url"`
	Installed   bool    `json:"installed"`
	Registered  bool    `json:"registered"`
	Status      string  `json:"status"`
	ProgressPct int     `json:"progress_pct"`
	Message     string  `json:"message"`
}

type catalogModel struct {
	ID          string
	DisplayName string
	Backend     string
	VRAMGB      float64
	SizeGB      float64
	Description string
	UseCase     string
	LibraryURL  string
}

type pullModelRequest struct {
	Model string `json:"model"`
}

var downloadCatalog = []catalogModel{
	{ID: "gemma2:2b", DisplayName: "Gemma 2 2B", Backend: "ollama", VRAMGB: 2.0, SizeGB: 1.6, Description: "Small general-purpose model for low VRAM systems", UseCase: "Fast chat", LibraryURL: "https://ollama.com/library/gemma2"},
	{ID: "phi3:mini", DisplayName: "Phi-3 Mini", Backend: "ollama", VRAMGB: 2.3, SizeGB: 2.3, Description: "Lightweight fallback model with strong instruction following", UseCase: "Fallback chat", LibraryURL: "https://ollama.com/library/phi3"},
	{ID: "llama3.2:3b", DisplayName: "Llama 3.2 3B", Backend: "ollama", VRAMGB: 3.0, SizeGB: 2.0, Description: "Efficient modern Llama model for everyday local chat", UseCase: "Balanced chat", LibraryURL: "https://ollama.com/library/llama3.2"},
	{ID: "deepseek-coder:6.7b", DisplayName: "DeepSeek Coder 6.7B", Backend: "ollama", VRAMGB: 4.2, SizeGB: 3.8, Description: "Code-focused model for generation and explanation", UseCase: "Code", LibraryURL: "https://ollama.com/library/deepseek-coder"},
	{ID: "mistral:7b", DisplayName: "Mistral 7B", Backend: "ollama", VRAMGB: 4.8, SizeGB: 4.1, Description: "Reliable general-purpose 7B model", UseCase: "General chat", LibraryURL: "https://ollama.com/library/mistral"},
	{ID: "qwen2.5:7b", DisplayName: "Qwen 2.5 7B", Backend: "ollama", VRAMGB: 5.0, SizeGB: 4.7, Description: "Strong multilingual and reasoning-capable local model", UseCase: "Reasoning", LibraryURL: "https://ollama.com/library/qwen2.5"},
	{ID: "llama3:8b", DisplayName: "Llama 3 8B", Backend: "ollama", VRAMGB: 5.5, SizeGB: 4.7, Description: "Fast general-purpose model with broad compatibility", UseCase: "General chat", LibraryURL: "https://ollama.com/library/llama3"},
	{ID: "codellama:7b", DisplayName: "Code Llama 7B", Backend: "ollama", VRAMGB: 5.5, SizeGB: 3.8, Description: "Local coding assistant for completion and refactoring", UseCase: "Code", LibraryURL: "https://ollama.com/library/codellama"},
}

func catalogByID(id string) (catalogModel, bool) {
	for _, item := range downloadCatalog {
		if item.ID == id {
			return item, true
		}
	}
	return catalogModel{}, false
}

func pullJobMap(jobs []lifecycle.PullJob) map[string]lifecycle.PullJob {
	out := make(map[string]lifecycle.PullJob, len(jobs))
	for _, job := range jobs {
		out[job.Model] = job
	}
	return out
}

func ollamaLibraryReachable(ctx context.Context) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, "https://ollama.com/library", nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode >= 200 && res.StatusCode < 500
}
