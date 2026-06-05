package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/db"
)

// NewTestDB opens an in-memory SQLite store and registers cleanup.
func NewTestDB(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("testutil.NewTestDB: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// NewTestConfig returns a Manager with test-safe defaults plus any overrides applied.
func NewTestConfig(t *testing.T, overrides func(*config.Config)) *config.Manager {
	t.Helper()
	cfg := config.Defaults()
	if overrides != nil {
		overrides(&cfg)
	}
	mgr, err := config.NewManagerFromConfig(cfg)
	if err != nil {
		t.Fatalf("testutil.NewTestConfig: %v", err)
	}
	return mgr
}

// SeedAdminUser creates a known admin account and returns its ID and plain password.
func SeedAdminUser(t *testing.T, store *db.Store) (userID, plainPassword string) {
	t.Helper()
	plainPassword = "test-password-" + t.Name()
	hash, salt, err := auth.HashPassword(plainPassword)
	if err != nil {
		t.Fatalf("testutil.SeedAdminUser hash: %v", err)
	}
	id, err := auth.GenerateID("usr")
	if err != nil {
		t.Fatalf("testutil.SeedAdminUser id: %v", err)
	}
	if err := store.CreateUser(context.Background(), db.User{
		ID:        id,
		Username:  "testadmin",
		Hash:      hash,
		Salt:      salt,
		Role:      "admin",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("testutil.SeedAdminUser create: %v", err)
	}
	return id, plainPassword
}

// SeedAPIKey creates a key of a given role and returns its ID and raw token.
func SeedAPIKey(t *testing.T, store *db.Store, keyRole string) (keyID, rawKey string) {
	t.Helper()
	if keyRole == "" {
		keyRole = "inference"
	}
	raw, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("testutil.SeedAPIKey gen: %v", err)
	}
	salt, err := auth.GenerateSalt()
	if err != nil {
		t.Fatalf("testutil.SeedAPIKey salt: %v", err)
	}
	id, err := auth.GenerateID("key")
	if err != nil {
		t.Fatalf("testutil.SeedAPIKey id: %v", err)
	}
	if err := store.CreateAPIKey(context.Background(), db.NewAPIKey{
		ID:        id,
		Label:     "test key " + keyRole,
		Salt:      salt,
		Hash:      auth.HashKey(raw, salt),
		KeyRole:   keyRole,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("testutil.SeedAPIKey create: %v", err)
	}
	return id, raw
}
