package hardware

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Info describes the detected GPU and current VRAM state.
type Info struct {
	GPUName     string  `json:"gpu_name"`
	VRAMTotalGB float64 `json:"vram_total_gb"`
	VRAMFreeGB  float64 `json:"vram_free_gb"`
	VRAMUsedGB  float64 `json:"vram_used_gb"`
	Temperature *int    `json:"temperature_c,omitempty"`
	Detected    bool    `json:"detected"`
}

// Provider queries local hardware state.
type Provider interface {
	Query(ctx context.Context) (Info, error)
}

// NVIDIAProvider queries nvidia-smi and gracefully falls back to CPU mode.
type NVIDIAProvider struct{}

// NewNVIDIAProvider creates a GPU hardware provider.
func NewNVIDIAProvider() *NVIDIAProvider {
	return &NVIDIAProvider{}
}

// Query returns current NVIDIA GPU memory information when available.
func (p *NVIDIAProvider) Query(ctx context.Context) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if info, ok := queryJSON(ctx); ok {
		return info, nil
	}
	if info, ok := queryCSV(ctx); ok {
		return info, nil
	}
	return Info{Detected: false}, nil
}

func queryJSON(ctx context.Context) (Info, bool) {
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total,memory.used,temperature.gpu", "--format=json").Output()
	if err != nil {
		return Info{}, false
	}
	var doc struct {
		GPU []struct {
			Name        string `json:"name"`
			MemoryTotal string `json:"memory.total"`
			MemoryUsed  string `json:"memory.used"`
			Temperature string `json:"temperature.gpu"`
		} `json:"gpu"`
	}
	if json.Unmarshal(out, &doc) != nil || len(doc.GPU) == 0 {
		return Info{}, false
	}
	gpu := doc.GPU[0]
	totalMB, err := parseNvidiaNumber(gpu.MemoryTotal)
	if err != nil {
		return Info{}, false
	}
	usedMB, err := parseNvidiaNumber(gpu.MemoryUsed)
	if err != nil {
		return Info{}, false
	}
	temp, _ := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(gpu.Temperature, " C")))
	return Info{
		GPUName:     strings.TrimSpace(gpu.Name),
		VRAMTotalGB: roundOne(totalMB / 1024),
		VRAMUsedGB:  roundOne(usedMB / 1024),
		VRAMFreeGB:  roundOne((totalMB - usedMB) / 1024),
		Temperature: &temp,
		Detected:    true,
	}, true
}

func queryCSV(ctx context.Context) (Info, bool) {
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total,memory.used,temperature.gpu", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return Info{}, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return Info{}, false
	}
	parts := strings.Split(lines[0], ",")
	if len(parts) < 4 {
		return Info{}, false
	}
	totalMB, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return Info{}, false
	}
	usedMB, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if err != nil {
		return Info{}, false
	}
	temp, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
	tempPtr := &temp
	totalGB := totalMB / 1024
	usedGB := usedMB / 1024
	return Info{
		GPUName:     strings.TrimSpace(parts[0]),
		VRAMTotalGB: roundOne(totalGB),
		VRAMUsedGB:  roundOne(usedGB),
		VRAMFreeGB:  roundOne(totalGB - usedGB),
		Temperature: tempPtr,
		Detected:    true,
	}, true
}

func roundOne(value float64) float64 {
	return float64(int(value*10+0.5)) / 10
}

func parseNvidiaNumber(value string) (float64, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "MiB")
	value = strings.TrimSuffix(value, "MB")
	return strconv.ParseFloat(strings.TrimSpace(value), 64)
}
