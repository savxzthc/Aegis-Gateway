package db

import (
	"context"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateUser_Unique(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	u := User{ID: "u1", Username: "alice", Hash: "h", Salt: "", Role: "admin", CreatedAt: time.Now().UTC()}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("first create: %v", err)
	}
	u2 := User{ID: "u2", Username: "alice", Hash: "h2", Salt: "", Role: "operator", CreatedAt: time.Now().UTC()}
	if err := store.CreateUser(ctx, u2); err != ErrDuplicateUsername {
		t.Errorf("expected ErrDuplicateUsername, got %v", err)
	}
}

func TestGetUserByUsername_Unknown(t *testing.T) {
	store := newTestStore(t)
	u, err := store.GetUserByUsername(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != nil {
		t.Error("expected nil for unknown user")
	}
}

func TestUpdateUserLastLogin(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	u := User{ID: "u1", Username: "bob", Hash: "h", Salt: "", Role: "operator", CreatedAt: time.Now().UTC()}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.UpdateUserLastLogin(ctx, "u1", now); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := store.GetUserByID(ctx, "u1")
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastLogin == nil {
		t.Fatal("expected LastLogin to be set")
	}
	if got.LastLogin.Unix() != now.Unix() {
		t.Errorf("expected %v, got %v", now, *got.LastLogin)
	}
}

func TestListUsers_SortedByCreatedAt(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	users := []User{
		{ID: "u1", Username: "alice", Hash: "h", Salt: "", Role: "admin", CreatedAt: now},
		{ID: "u2", Username: "bob", Hash: "h", Salt: "", Role: "operator", CreatedAt: now.Add(time.Second)},
	}
	for _, u := range users {
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatalf("create %s: %v", u.Username, err)
		}
	}
	list, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 users, got %d", len(list))
	}
	if list[0].Username != "alice" {
		t.Errorf("expected first user alice, got %s", list[0].Username)
	}
}
