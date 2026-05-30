package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreKeyLifecycleAndStats(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.HealthCheck(ctx); err != nil {
		t.Fatal(err)
	}

	count, err := store.CountActiveKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("got %d active keys, want 0", count)
	}

	created := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	if err := store.CreateAPIKey(ctx, NewAPIKey{
		ID:        "key_test",
		Label:     "Test",
		Salt:      "salt",
		Hash:      "hash",
		CreatedAt: created,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkKeyUsed(ctx, "key_test", created.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	keys, err := store.ListAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].RequestsTotal != 1 || keys[0].LastUsed == nil {
		t.Fatalf("unexpected keys: %#v", keys)
	}

	if err := store.InsertRequestLog(ctx, RequestLog{
		Timestamp:                 created,
		KeyID:                     "key_test",
		ModelRequested:            "llama3:8b",
		ModelUsed:                 "phi3:mini",
		FallbackTriggered:         true,
		BackendType:               "ollama",
		LatencyMS:                 250,
		EstimatedPromptTokens:     10,
		EstimatedCompletionTokens: 20,
		StatusCode:                200,
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := store.GetStats(ctx, created)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RequestsToday != 1 || stats.RequestsTotal != 1 || stats.AvgLatencyMS != 250 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats.FallbackRatePct != 100 {
		t.Fatalf("got fallback rate %f, want 100", stats.FallbackRatePct)
	}
	if len(stats.RequestsPerHour) != 24 {
		t.Fatalf("got %d hourly buckets, want 24", len(stats.RequestsPerHour))
	}

	revoked, err := store.RevokeAPIKey(ctx, "key_test", created.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("expected key to revoke")
	}
	keys, err = store.ListAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("revoked key still listed: %#v", keys)
	}
}

func TestRevokeAPIKeyIfNotLastPreventsLockout(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	created := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	if err := store.CreateAPIKey(ctx, NewAPIKey{
		ID:        "key_only",
		Label:     "Only",
		Salt:      "salt",
		Hash:      "hash",
		CreatedAt: created,
	}); err != nil {
		t.Fatal(err)
	}

	revoked, last, err := store.RevokeAPIKeyIfNotLast(ctx, "key_only", created.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if revoked || !last {
		t.Fatalf("got revoked=%v last=%v, want final-key denial", revoked, last)
	}
	count, err := store.CountActiveKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("got %d active keys, want 1", count)
	}

	if err := store.CreateAPIKey(ctx, NewAPIKey{
		ID:        "key_second",
		Label:     "Second",
		Salt:      "salt",
		Hash:      "hash",
		CreatedAt: created,
	}); err != nil {
		t.Fatal(err)
	}
	revoked, last, err = store.RevokeAPIKeyIfNotLast(ctx, "key_only", created.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !revoked || last {
		t.Fatalf("got revoked=%v last=%v, want revoke", revoked, last)
	}
}

func TestResetAPIKeysRevokesExistingAndCreatesReplacement(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	created := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"key_one", "key_two"} {
		if err := store.CreateAPIKey(ctx, NewAPIKey{ID: id, Label: id, Salt: "salt", Hash: "hash", CreatedAt: created}); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.ResetAPIKeys(ctx, NewAPIKey{
		ID:        "key_recovery",
		Label:     "Recovery",
		Salt:      "salt2",
		Hash:      "hash2",
		CreatedAt: created.Add(time.Minute),
	}, created.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	keys, err := store.ListAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != "key_recovery" {
		t.Fatalf("unexpected active keys: %#v", keys)
	}
	secrets, err := store.ActiveKeySecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(secrets) != 1 || secrets[0].ID != "key_recovery" {
		t.Fatalf("unexpected active secrets: %#v", secrets)
	}
}

func TestActiveKeySecretsCacheInvalidatesOnKeyChanges(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	created := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	if err := store.CreateAPIKey(ctx, NewAPIKey{ID: "key_one", Label: "One", Salt: "salt", Hash: "hash", CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	keys, err := store.ActiveKeySecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys))
	}
	if err := store.CreateAPIKey(ctx, NewAPIKey{ID: "key_two", Label: "Two", Salt: "salt", Hash: "hash", CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	keys, err = store.ActiveKeySecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("cache was not invalidated after create: %#v", keys)
	}
	if revoked, err := store.RevokeAPIKey(ctx, "key_one", created.Add(time.Minute)); err != nil || !revoked {
		t.Fatalf("revoke failed revoked=%v err=%v", revoked, err)
	}
	keys, err = store.ActiveKeySecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != "key_two" {
		t.Fatalf("cache was not invalidated after revoke: %#v", keys)
	}
}

func TestPruneOldMetadataRemovesExpiredRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	if err := store.InsertRequestLog(ctx, RequestLog{
		Timestamp:                 now.Add(-requestLogRetention - time.Hour),
		KeyID:                     "key_old",
		ModelRequested:            "old",
		ModelUsed:                 "old",
		BackendType:               "ollama",
		LatencyMS:                 1,
		EstimatedPromptTokens:     1,
		EstimatedCompletionTokens: 1,
		StatusCode:                200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestLog(ctx, RequestLog{
		Timestamp:                 now.Add(-requestLogRetention + time.Hour),
		KeyID:                     "key_new",
		ModelRequested:            "new",
		ModelUsed:                 "new",
		BackendType:               "ollama",
		LatencyMS:                 1,
		EstimatedPromptTokens:     1,
		EstimatedCompletionTokens: 1,
		StatusCode:                200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.LogAuthFailure(ctx, "old-ip", now.Add(-authFailureRetention-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.LogAuthFailure(ctx, "new-ip", now.Add(-authFailureRetention+time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := store.PruneOldMetadata(ctx, now); err != nil {
		t.Fatal(err)
	}

	logs, total, err := store.ListRequestLogs(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ModelRequested != "new" {
		t.Fatalf("unexpected retained logs: %#v", logs)
	}
	if total != 1 {
		t.Fatalf("got %d request logs, want 1", total)
	}
	authFailures := countRows(t, store, `SELECT COUNT(*) FROM auth_failures`)
	if authFailures != 1 {
		t.Fatalf("got %d auth failures, want 1", authFailures)
	}
}

func TestMetadataPruneIsCadenced(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	if err := store.maybePruneOldMetadata(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	first := store.lastMetadataPrune
	if err := store.maybePruneOldMetadata(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !store.lastMetadataPrune.Equal(first) {
		t.Fatalf("prune ran too soon: %s then %s", first, store.lastMetadataPrune)
	}
	if err := store.maybePruneOldMetadata(context.Background(), now.Add(metadataPruneEvery+time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !store.lastMetadataPrune.After(first) {
		t.Fatalf("prune did not advance: %s then %s", first, store.lastMetadataPrune)
	}
}

func TestFormatTimeUsesSortableFixedWidthUTC(t *testing.T) {
	early := time.Date(2026, 5, 29, 12, 0, 0, 500, time.UTC)
	late := time.Date(2026, 5, 29, 12, 0, 1, 0, time.UTC)
	earlyText := formatTime(early)
	lateText := formatTime(late)
	if len(earlyText) != len(lateText) {
		t.Fatalf("time strings are not fixed width: %q %q", earlyText, lateText)
	}
	if !(earlyText < lateText) {
		t.Fatalf("time strings are not lexically sortable: %q >= %q", earlyText, lateText)
	}
	parsed, err := parseTime(earlyText)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Equal(early) {
		t.Fatalf("parsed %s, want %s", parsed, early)
	}
	if _, err := parseTime("2026-05-29T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, store *Store, query string) int {
	t.Helper()
	var count int
	if err := store.conn.QueryRowContext(context.Background(), query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
