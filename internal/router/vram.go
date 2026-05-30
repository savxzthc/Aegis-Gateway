package router

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/hardware"
)

// ErrModelNotRegistered means the requested model is absent from the registry.
var ErrModelNotRegistered = errors.New("model is not registered")

// ErrNoModelFits means no registered model can fit available VRAM.
var ErrNoModelFits = errors.New("no registered model fits available VRAM")

// VRAMRouter selects the best registered model for current GPU memory.
type VRAMRouter struct {
	cfg      *config.Manager
	hardware hardware.Provider
}

// NewVRAMRouter creates a VRAM-aware model router.
func NewVRAMRouter(cfg *config.Manager, hardware hardware.Provider) *VRAMRouter {
	return &VRAMRouter{cfg: cfg, hardware: hardware}
}

// SelectModel returns the model that should run and whether fallback happened.
func (r *VRAMRouter) SelectModel(ctx context.Context, requested string) (string, bool, error) {
	cfg := r.cfg.Get()
	requestedModel, ok := cfg.Models.Registry[requested]
	if !ok {
		return "", false, fmt.Errorf("%w: %s", ErrModelNotRegistered, requested)
	}

	info, err := r.hardware.Query(ctx)
	if err != nil || !info.Detected {
		smallest := smallestModel(cfg.Models.Registry)
		log.Printf("aegis: no GPU detected, routing %s to smallest registered model %s", requested, smallest)
		return smallest, smallest != requested, nil
	}

	if requestedModel.VRAMGB <= info.VRAMFreeGB {
		return requested, false, nil
	}

	candidates := sortedByVRAMDesc(cfg.Models.Registry)
	for _, candidate := range candidates {
		if cfg.Models.Registry[candidate].VRAMGB <= info.VRAMFreeGB {
			return candidate, candidate != requested, nil
		}
	}
	return "", false, fmt.Errorf("%w: %.1f GB free", ErrNoModelFits, info.VRAMFreeGB)
}

func smallestModel(registry map[string]config.ModelConfig) string {
	names := config.SortedModels(registry)
	smallest := names[0]
	for _, name := range names[1:] {
		if registry[name].VRAMGB < registry[smallest].VRAMGB {
			smallest = name
		}
	}
	return smallest
}

func sortedByVRAMDesc(registry map[string]config.ModelConfig) []string {
	names := config.SortedModels(registry)
	sort.SliceStable(names, func(i, j int) bool {
		return registry[names[i]].VRAMGB > registry[names[j]].VRAMGB
	})
	return names
}
