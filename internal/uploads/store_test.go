package uploads

import (
	"context"
	"os"
	"testing"
)

func TestStoreRejectsPathTraversal(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "../../etc/passwd", "text/plain", []byte("no")); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOwned(context.Background(), "upl_test123", "image/png", "owner", []byte("png")); err != nil {
		t.Fatal(err)
	}
	data, contentType, err := store.Load(context.Background(), "upl_test123")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "png" || contentType != "image/png" {
		t.Fatalf("unexpected upload %q %q", data, contentType)
	}
	if err := store.Delete(context.Background(), "upl_test123"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(context.Background(), "upl_test123"); !os.IsNotExist(err) {
		t.Fatalf("expected not exist, got %v", err)
	}
}
