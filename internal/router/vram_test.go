package router

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/hardware"
)

type stubHardware struct {
	info hardware.Info
	err  error
}

func (s stubHardware) Query(ctx context.Context) (hardware.Info, error) {
	return s.info, s.err
}

func TestSelectModelRequestedFits(t *testing.T) {
	cfg := testConfig(t)
	router := NewVRAMRouter(cfg, stubHardware{info: hardware.Info{Detected: true, VRAMFreeGB: 6.0}})

	model, fallback, err := router.SelectModel(context.Background(), "llama3:8b")
	if err != nil {
		t.Fatal(err)
	}
	if model != "llama3:8b" || fallback {
		t.Fatalf("got model=%q fallback=%v", model, fallback)
	}
}

func TestSelectModelFallsBackToLargestFit(t *testing.T) {
	cfg := testConfig(t)
	router := NewVRAMRouter(cfg, stubHardware{info: hardware.Info{Detected: true, VRAMFreeGB: 4.5}})

	model, fallback, err := router.SelectModel(context.Background(), "llama3:8b")
	if err != nil {
		t.Fatal(err)
	}
	if model != "deepseek-coder:6.7b" || !fallback {
		t.Fatalf("got model=%q fallback=%v", model, fallback)
	}
}

func TestSelectModelNoGPUUsesSmallest(t *testing.T) {
	cfg := testConfig(t)
	router := NewVRAMRouter(cfg, stubHardware{info: hardware.Info{Detected: false}})

	model, fallback, err := router.SelectModel(context.Background(), "llama3:8b")
	if err != nil {
		t.Fatal(err)
	}
	if model != "phi3:mini" || !fallback {
		t.Fatalf("got model=%q fallback=%v", model, fallback)
	}
}

func TestSelectModelUnknownModelUsesSentinel(t *testing.T) {
	cfg := testConfig(t)
	router := NewVRAMRouter(cfg, stubHardware{info: hardware.Info{Detected: true, VRAMFreeGB: 16}})

	_, _, err := router.SelectModel(context.Background(), "missing")
	if !errors.Is(err, ErrModelNotRegistered) {
		t.Fatalf("got %v, want ErrModelNotRegistered", err)
	}
}

func TestSelectModelNoFitUsesSentinel(t *testing.T) {
	cfg := testConfig(t)
	router := NewVRAMRouter(cfg, stubHardware{info: hardware.Info{Detected: true, VRAMFreeGB: 1.0}})

	_, _, err := router.SelectModel(context.Background(), "llama3:8b")
	if !errors.Is(err, ErrNoModelFits) {
		t.Fatalf("got %v, want ErrNoModelFits", err)
	}
}

func testConfig(t *testing.T) *config.Manager {
	t.Helper()
	cfg, err := config.LoadManager(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
