package backends

import (
	"testing"
)

func TestValidateBackendURL_Localhost(t *testing.T) {
	urls := []string{
		"http://127.0.0.1:11434",
		"http://localhost:8080",
		"http://[::1]:8080",
	}
	for _, u := range urls {
		if err := ValidateBackendURL(u); err != nil {
			t.Errorf("localhost URL %q should be allowed: %v", u, err)
		}
	}
}

func TestValidateBackendURL_HTTPS_Remote(t *testing.T) {
	if err := ValidateBackendURL("https://api.openai.com/v1"); err != nil {
		t.Errorf("HTTPS remote URL should be allowed: %v", err)
	}
}

func TestValidateBackendURL_HTTP_Remote_Rejected(t *testing.T) {
	if err := ValidateBackendURL("http://api.openai.com/v1"); err == nil {
		t.Error("HTTP remote URL should be rejected")
	}
}

func TestValidateBackendURL_Private_IP_Rejected(t *testing.T) {
	privates := []string{
		"https://192.168.1.1/api",
		"https://10.0.0.1/api",
		"https://172.16.0.1/api",
	}
	for _, u := range privates {
		if err := ValidateBackendURL(u); err == nil {
			t.Errorf("private IP %q should be rejected", u)
		}
	}
}

func TestValidateBackendURL_InvalidScheme(t *testing.T) {
	if err := ValidateBackendURL("ftp://example.com"); err == nil {
		t.Error("ftp scheme should be rejected")
	}
}
