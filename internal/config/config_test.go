package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManagerWritesDefaultConfigWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := LoadManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Get().Server.Port != 9000 {
		t.Fatalf("unexpected default port %d", manager.Get().Server.Port)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"[server]",
		"# The host and port Aegis listens on.",
		`[models.registry."llama3:8b"]`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("default config missing %q:\n%s", want, text)
		}
	}
}

func TestPatchEditableWritesCommentedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := LoadManager(path)
	if err != nil {
		t.Fatal(err)
	}

	port := 19000
	cfg, err := manager.PatchEditable(EditablePatch{Port: &port})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != port {
		t.Fatalf("got port %d", cfg.Server.Port)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"# The host and port Aegis listens on.",
		"# Maximum requests per minute per API key.",
		"# Model registry.",
		`[models.registry."llama3:8b"]`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("written config missing %q:\n%s", want, text)
		}
	}
}
