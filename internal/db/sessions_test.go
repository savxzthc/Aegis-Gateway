package db

import (
	"context"
	"testing"
	"time"
)

func seedUserForSession(t *testing.T, store *Store) string {
	t.Helper()
	u := User{ID: "u1", Username: "sess_user", Hash: "h", Salt: "", Role: "admin", CreatedAt: time.Now().UTC()}
	if err := store.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return u.ID
}

func TestCreateSession_Stored(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	userID := seedUserForSession(t, store)
	now := time.Now().UTC()
	sess := Session{Token: "tok1", UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.GetSession(ctx, "tok1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.UserID != userID {
		t.Errorf("expected session with user %s, got %v", userID, got)
	}
}

func TestGetSession_Expired(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	userID := seedUserForSession(t, store)
	past := time.Now().UTC().Add(-time.Hour)
	sess := Session{Token: "expired", UserID: userID, ExpiresAt: past, CreatedAt: past}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.GetSession(ctx, "expired")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Error("expected nil for expired session")
	}
}

func TestGetSession_Unknown(t *testing.T) {
	store := newTestStore(t)
	got, err := store.GetSession(context.Background(), "unknown-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for unknown token")
	}
}

func TestDeleteSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	userID := seedUserForSession(t, store)
	now := time.Now().UTC()
	sess := Session{Token: "del-tok", UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.DeleteSession(ctx, "del-tok"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := store.GetSession(ctx, "del-tok")
	if got != nil {
		t.Error("expected nil after deletion")
	}
}

func TestPruneSessions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	userID := seedUserForSession(t, store)
	now := time.Now().UTC()
	expired := Session{Token: "exp", UserID: userID, ExpiresAt: now.Add(-time.Hour), CreatedAt: now.Add(-2 * time.Hour)}
	active := Session{Token: "active", UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	_ = store.CreateSession(ctx, expired)
	_ = store.CreateSession(ctx, active)
	if err := store.PruneSessions(ctx); err != nil {
		t.Fatalf("prune: %v", err)
	}
	got, _ := store.GetSession(ctx, "active")
	if got == nil {
		t.Error("expected active session to survive prune")
	}
}
