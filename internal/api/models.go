package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
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
	category := normalizeCatalogCategory(r.URL.Query().Get("category"))
	limit := parseCatalogLimit(r.URL.Query().Get("limit"))
	offset := parseCatalogOffset(r.URL.Query().Get("offset"))
	installedModels := map[string]bool{}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	installedModels = s.Lifecycle.OllamaModels(ctx)
	cancel()
	data := make([]catalogModelObject, 0, len(downloadCatalog))
	for _, item := range downloadCatalog {
		if category != "" && item.Category != category {
			continue
		}
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
			Category:    item.Category,
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
	total := len(data)
	end := offset + limit
	if offset > total {
		offset = total
	}
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, catalogResponse{
		Online:   online,
		Data:     data[offset:end],
		Total:    total,
		Limit:    limit,
		Offset:   offset,
		Category: category,
	})
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
	Online   bool                 `json:"online"`
	Data     []catalogModelObject `json:"data"`
	Total    int                  `json:"total"`
	Limit    int                  `json:"limit"`
	Offset   int                  `json:"offset"`
	Category string               `json:"category"`
}

type catalogModelObject struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Backend     string  `json:"backend"`
	VRAMGB      float64 `json:"vram_gb"`
	SizeGB      float64 `json:"size_gb"`
	Description string  `json:"description"`
	UseCase     string  `json:"use_case"`
	Category    string  `json:"category"`
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
	Category    string
	LibraryURL  string
}

type pullModelRequest struct {
	Model string `json:"model"`
}

var downloadCatalog = []catalogModel{
	{ID: "tinyllama:1.1b", DisplayName: "TinyLlama 1.1B", Backend: "ollama", VRAMGB: 1.2, SizeGB: 0.7, Description: "Very small model for quick smoke tests and low-memory machines", UseCase: "Tiny chat", Category: "standard", LibraryURL: "https://ollama.com/library/tinyllama"},
	{ID: "qwen2.5:0.5b", DisplayName: "Qwen 2.5 0.5B", Backend: "ollama", VRAMGB: 1.4, SizeGB: 0.4, Description: "Ultra-light multilingual model for basic local chat", UseCase: "Tiny chat", Category: "standard", LibraryURL: "https://ollama.com/library/qwen2.5"},
	{ID: "smollm2:1.7b", DisplayName: "SmolLM2 1.7B", Backend: "ollama", VRAMGB: 1.8, SizeGB: 1.1, Description: "Compact assistant model with good latency on modest hardware", UseCase: "Fast chat", Category: "standard", LibraryURL: "https://ollama.com/library/smollm2"},
	{ID: "gemma2:2b", DisplayName: "Gemma 2 2B", Backend: "ollama", VRAMGB: 2.0, SizeGB: 1.6, Description: "Small general-purpose model for low VRAM systems", UseCase: "Fast chat", Category: "standard", LibraryURL: "https://ollama.com/library/gemma2"},
	{ID: "llama3.2:1b", DisplayName: "Llama 3.2 1B", Backend: "ollama", VRAMGB: 2.0, SizeGB: 1.3, Description: "Small modern Llama model for very fast responses", UseCase: "Fast chat", Category: "standard", LibraryURL: "https://ollama.com/library/llama3.2"},
	{ID: "phi3:mini", DisplayName: "Phi-3 Mini", Backend: "ollama", VRAMGB: 2.3, SizeGB: 2.3, Description: "Lightweight fallback model with strong instruction following", UseCase: "Fallback chat", Category: "standard", LibraryURL: "https://ollama.com/library/phi3"},
	{ID: "qwen2.5:1.5b", DisplayName: "Qwen 2.5 1.5B", Backend: "ollama", VRAMGB: 2.5, SizeGB: 1.0, Description: "Small multilingual model with practical reasoning ability", UseCase: "Multilingual", Category: "standard", LibraryURL: "https://ollama.com/library/qwen2.5"},
	{ID: "qwen2.5-coder:1.5b", DisplayName: "Qwen 2.5 Coder 1.5B", Backend: "ollama", VRAMGB: 2.6, SizeGB: 1.0, Description: "Tiny code model for quick edits and explanations", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/qwen2.5-coder"},
	{ID: "llama3.2:3b", DisplayName: "Llama 3.2 3B", Backend: "ollama", VRAMGB: 3.0, SizeGB: 2.0, Description: "Efficient modern Llama model for everyday local chat", UseCase: "Balanced chat", Category: "standard", LibraryURL: "https://ollama.com/library/llama3.2"},
	{ID: "qwen2.5:3b", DisplayName: "Qwen 2.5 3B", Backend: "ollama", VRAMGB: 3.2, SizeGB: 1.9, Description: "Efficient multilingual assistant for consumer GPUs", UseCase: "Multilingual", Category: "standard", LibraryURL: "https://ollama.com/library/qwen2.5"},
	{ID: "qwen2.5-coder:3b", DisplayName: "Qwen 2.5 Coder 3B", Backend: "ollama", VRAMGB: 3.3, SizeGB: 1.9, Description: "Small coding model for snippets and local development help", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/qwen2.5-coder"},
	{ID: "codegemma:2b", DisplayName: "CodeGemma 2B", Backend: "ollama", VRAMGB: 3.5, SizeGB: 1.6, Description: "Compact code completion and explanation model", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/codegemma"},
	{ID: "starcoder2:3b", DisplayName: "StarCoder2 3B", Backend: "ollama", VRAMGB: 3.8, SizeGB: 1.7, Description: "Lightweight code-specialized model for local coding tasks", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/starcoder2"},
	{ID: "deepseek-coder:6.7b", DisplayName: "DeepSeek Coder 6.7B", Backend: "ollama", VRAMGB: 4.2, SizeGB: 3.8, Description: "Code-focused model for generation and explanation", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/deepseek-coder"},
	{ID: "neural-chat:7b", DisplayName: "Neural Chat 7B", Backend: "ollama", VRAMGB: 4.5, SizeGB: 4.1, Description: "Instruction-tuned chat model for general local use", UseCase: "General chat", Category: "standard", LibraryURL: "https://ollama.com/library/neural-chat"},
	{ID: "mistral:7b", DisplayName: "Mistral 7B", Backend: "ollama", VRAMGB: 4.8, SizeGB: 4.1, Description: "Reliable general-purpose 7B model", UseCase: "General chat", Category: "standard", LibraryURL: "https://ollama.com/library/mistral"},
	{ID: "qwen2.5:7b", DisplayName: "Qwen 2.5 7B", Backend: "ollama", VRAMGB: 5.0, SizeGB: 4.7, Description: "Strong multilingual and reasoning-capable local model", UseCase: "Reasoning", Category: "standard", LibraryURL: "https://ollama.com/library/qwen2.5"},
	{ID: "gemma3:4b", DisplayName: "Gemma 3 4B", Backend: "ollama", VRAMGB: 5.0, SizeGB: 3.3, Description: "Modern Gemma model with strong quality for its size", UseCase: "Balanced chat", Category: "standard", LibraryURL: "https://ollama.com/library/gemma3"},
	{ID: "llama3:8b", DisplayName: "Llama 3 8B", Backend: "ollama", VRAMGB: 5.5, SizeGB: 4.7, Description: "Fast general-purpose model with broad compatibility", UseCase: "General chat", Category: "standard", LibraryURL: "https://ollama.com/library/llama3"},
	{ID: "llama3.1:8b", DisplayName: "Llama 3.1 8B", Backend: "ollama", VRAMGB: 5.5, SizeGB: 4.9, Description: "Updated Llama 8B model for broad assistant workloads", UseCase: "General chat", Category: "standard", LibraryURL: "https://ollama.com/library/llama3.1"},
	{ID: "codellama:7b", DisplayName: "Code Llama 7B", Backend: "ollama", VRAMGB: 5.5, SizeGB: 3.8, Description: "Local coding assistant for completion and refactoring", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/codellama"},
	{ID: "qwen2.5-coder:7b", DisplayName: "Qwen 2.5 Coder 7B", Backend: "ollama", VRAMGB: 5.8, SizeGB: 4.7, Description: "Strong local coding model for practical development", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/qwen2.5-coder"},
	{ID: "codegemma:7b", DisplayName: "CodeGemma 7B", Backend: "ollama", VRAMGB: 6.0, SizeGB: 5.0, Description: "Code-focused model from the Gemma family", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/codegemma"},
	{ID: "starcoder2:7b", DisplayName: "StarCoder2 7B", Backend: "ollama", VRAMGB: 6.2, SizeGB: 4.0, Description: "Code model for generation, completion, and review", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/starcoder2"},
	{ID: "dolphin3:8b", DisplayName: "Dolphin 3 8B", Backend: "ollama", VRAMGB: 6.4, SizeGB: 4.7, Description: "General assistant model tuned for direct conversation", UseCase: "Chat", Category: "standard", LibraryURL: "https://ollama.com/library/dolphin3"},
	{ID: "deepseek-r1:1.5b", DisplayName: "DeepSeek R1 1.5B", Backend: "ollama", VRAMGB: 2.8, SizeGB: 1.1, Description: "Small reasoning model for low-memory experimentation", UseCase: "Reasoning", Category: "standard", LibraryURL: "https://ollama.com/library/deepseek-r1"},
	{ID: "deepseek-r1:7b", DisplayName: "DeepSeek R1 7B", Backend: "ollama", VRAMGB: 6.0, SizeGB: 4.7, Description: "Reasoning-focused model that runs on many consumer GPUs", UseCase: "Reasoning", Category: "standard", LibraryURL: "https://ollama.com/library/deepseek-r1"},
	{ID: "deepseek-r1:8b", DisplayName: "DeepSeek R1 8B", Backend: "ollama", VRAMGB: 6.4, SizeGB: 4.9, Description: "Reasoning model with a stronger quality target than tiny variants", UseCase: "Reasoning", Category: "standard", LibraryURL: "https://ollama.com/library/deepseek-r1"},
	{ID: "mistral-nemo:12b", DisplayName: "Mistral Nemo 12B", Backend: "ollama", VRAMGB: 8.0, SizeGB: 7.1, Description: "Higher-capacity Mistral-family model for 12GB and 16GB systems", UseCase: "General chat", Category: "standard", LibraryURL: "https://ollama.com/library/mistral-nemo"},
	{ID: "gemma3:12b", DisplayName: "Gemma 3 12B", Backend: "ollama", VRAMGB: 8.5, SizeGB: 8.1, Description: "Larger Gemma option for stronger local quality", UseCase: "General chat", Category: "standard", LibraryURL: "https://ollama.com/library/gemma3"},
	{ID: "qwen2.5:14b", DisplayName: "Qwen 2.5 14B", Backend: "ollama", VRAMGB: 9.5, SizeGB: 9.0, Description: "Larger multilingual model for 16GB VRAM systems", UseCase: "Reasoning", Category: "standard", LibraryURL: "https://ollama.com/library/qwen2.5"},
	{ID: "qwen2.5-coder:14b", DisplayName: "Qwen 2.5 Coder 14B", Backend: "ollama", VRAMGB: 10.0, SizeGB: 9.0, Description: "Larger local coding model for more complex codebases", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/qwen2.5-coder"},
	{ID: "starcoder2:15b", DisplayName: "StarCoder2 15B", Backend: "ollama", VRAMGB: 11.0, SizeGB: 9.1, Description: "Larger coding model for 16GB-class machines", UseCase: "Code", Category: "standard", LibraryURL: "https://ollama.com/library/starcoder2"},
	{ID: "llama3.3:70b", DisplayName: "Llama 3.3 70B", Backend: "ollama", VRAMGB: 40.0, SizeGB: 43.0, Description: "Large Llama model for high-memory machines or CPU-offload experiments", UseCase: "Large chat", Category: "standard", LibraryURL: "https://ollama.com/library/llama3.3"},
	{ID: "socialnetwooky/llama3.2-abliterated:1b_q4_k_m", DisplayName: "Llama 3.2 Abliterated 1B Q4", Backend: "ollama", VRAMGB: 2.0, SizeGB: 0.9, Description: "Small abliterated Llama 3.2 variant for lightweight local testing", UseCase: "Abliterated chat", Category: "abliterated", LibraryURL: "https://ollama.com/socialnetwooky/llama3.2-abliterated"},
	{ID: "socialnetwooky/llama3.2-abliterated:3b_q4_k_m", DisplayName: "Llama 3.2 Abliterated 3B Q4", Backend: "ollama", VRAMGB: 3.2, SizeGB: 2.0, Description: "Abliterated 3B Llama-family model for consumer GPUs", UseCase: "Abliterated chat", Category: "abliterated", LibraryURL: "https://ollama.com/socialnetwooky/llama3.2-abliterated"},
	{ID: "socialnetwooky/llama3.2-abliterated:3b_q8_0", DisplayName: "Llama 3.2 Abliterated 3B Q8", Backend: "ollama", VRAMGB: 4.6, SizeGB: 3.4, Description: "Higher-precision abliterated Llama 3.2 3B variant", UseCase: "Abliterated chat", Category: "abliterated", LibraryURL: "https://ollama.com/socialnetwooky/llama3.2-abliterated"},
	{ID: "huihui_ai/llama3.2-abliterate:1b", DisplayName: "Huihui Llama 3.2 Abliterate 1B", Backend: "ollama", VRAMGB: 2.0, SizeGB: 1.0, Description: "Small abliterated Llama 3.2 model from huihui_ai", UseCase: "Abliterated chat", Category: "abliterated", LibraryURL: "https://ollama.com/huihui_ai/llama3.2-abliterate"},
	{ID: "huihui_ai/llama3.2-abliterate:3b", DisplayName: "Huihui Llama 3.2 Abliterate 3B", Backend: "ollama", VRAMGB: 3.4, SizeGB: 2.1, Description: "Consumer-friendly abliterated Llama 3.2 3B model", UseCase: "Abliterated chat", Category: "abliterated", LibraryURL: "https://ollama.com/huihui_ai/llama3.2-abliterate"},
	{ID: "BlackHillsInfoSec/llama-3.1-8b-abliterated", DisplayName: "Llama 3.1 8B Abliterated", Backend: "ollama", VRAMGB: 6.5, SizeGB: 4.9, Description: "Abliterated Llama 3.1 8B variant for local security and research workflows", UseCase: "Abliterated chat", Category: "abliterated", LibraryURL: "https://ollama.com/BlackHillsInfoSec/llama-3.1-8b-abliterated"},
	{ID: "TheAzazel/l3.2-rogue-creative-instruct-abliterated-7b", DisplayName: "Rogue Creative Instruct Abliterated 7B", Backend: "ollama", VRAMGB: 6.8, SizeGB: 4.8, Description: "Abliterated creative-instruct model in the 7B class", UseCase: "Creative", Category: "abliterated", LibraryURL: "https://ollama.com/TheAzazel/l3.2-rogue-creative-instruct-abliterated-7b"},
	{ID: "huihui_ai/gemma3-abliterated:4b", DisplayName: "Gemma 3 Abliterated 4B", Backend: "ollama", VRAMGB: 5.2, SizeGB: 3.3, Description: "Abliterated Gemma-family option for mid-range GPUs", UseCase: "Abliterated chat", Category: "abliterated", LibraryURL: "https://ollama.com/huihui_ai/gemma3-abliterated"},
	{ID: "huihui_ai/qwq-abliterated", DisplayName: "QwQ Abliterated", Backend: "ollama", VRAMGB: 18.0, SizeGB: 18.0, Description: "Large abliterated reasoning model for high-memory systems", UseCase: "Reasoning", Category: "abliterated", LibraryURL: "https://ollama.com/huihui_ai/qwq-abliterated"},
	{ID: "huihui_ai/glm-4.7-flash-abliterated", DisplayName: "GLM 4.7 Flash Abliterated", Backend: "ollama", VRAMGB: 20.0, SizeGB: 18.0, Description: "Large abliterated GLM-family model for higher-memory hardware", UseCase: "Large chat", Category: "abliterated", LibraryURL: "https://ollama.com/huihui_ai/glm-4.7-flash-abliterated"},
	{ID: "vatistasdim/Cipher-Abliterated", DisplayName: "Cipher Abliterated", Backend: "ollama", VRAMGB: 6.5, SizeGB: 4.8, Description: "Abliterated conversational model published through Ollama", UseCase: "Abliterated chat", Category: "abliterated", LibraryURL: "https://ollama.com/vatistasdim/Cipher-Abliterated"},
}

func normalizeCatalogCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all", "standard":
		return "standard"
	case "abliterated":
		return "abliterated"
	default:
		return "standard"
	}
}

func parseCatalogLimit(value string) int {
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return 12
	}
	if limit > 48 {
		return 48
	}
	return limit
}

func parseCatalogOffset(value string) int {
	offset, err := strconv.Atoi(value)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
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
