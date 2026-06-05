package auth

import (
	"testing"
)

func TestHashPassword_Unique(t *testing.T) {
	h1, _, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("hash1: %v", err)
	}
	h2, _, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("hash2: %v", err)
	}
	if h1 == h2 {
		t.Error("expected different hashes for same password (salt randomness)")
	}
}

func TestVerifyPassword_Correct(t *testing.T) {
	hash, salt, err := HashPassword("correcthorse")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !VerifyPassword("correcthorse", hash, salt) {
		t.Error("expected VerifyPassword to return true for correct password")
	}
}

func TestVerifyPassword_Wrong(t *testing.T) {
	hash, salt, err := HashPassword("correcthorse")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if VerifyPassword("wrongpassword", hash, salt) {
		t.Error("expected VerifyPassword to return false for wrong password")
	}
}

func TestVerifyPassword_Empty(t *testing.T) {
	hash, salt, err := HashPassword("correcthorse")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if VerifyPassword("", hash, salt) {
		t.Error("expected VerifyPassword to return false for empty password")
	}
}
