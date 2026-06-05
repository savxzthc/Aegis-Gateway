package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestValidateTOTP_Valid(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("testuser", "Aegis Gateway")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if !ValidateTOTP(secret, code) {
		t.Error("expected ValidateTOTP to return true for fresh code")
	}
}

func TestValidateTOTP_Stale(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("testuser", "Aegis Gateway")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	oldCode, err := totp.GenerateCode(secret, time.Now().Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if ValidateTOTP(secret, oldCode) {
		t.Error("expected ValidateTOTP to return false for stale code")
	}
}

func TestValidateTOTP_Empty(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("testuser", "Aegis Gateway")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if ValidateTOTP(secret, "") {
		t.Error("expected ValidateTOTP to return false for empty code")
	}
}
