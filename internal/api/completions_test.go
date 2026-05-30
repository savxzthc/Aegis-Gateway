package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/savxzthc/aegis-gateway/internal/backends"
	modelrouter "github.com/savxzthc/aegis-gateway/internal/router"
)

func TestValidateChatRequest(t *testing.T) {
	valid := &backends.ChatRequest{
		Model:    "llama3:8b",
		Messages: []backends.ChatMessage{{Role: "user", Content: backends.NewMessageContent("hello")}},
	}
	if err := validateChatRequest(valid); err != nil {
		t.Fatal(err)
	}

	invalid := &backends.ChatRequest{
		Model:    "llama3:8b",
		Messages: []backends.ChatMessage{{Role: "invalid", Content: backends.NewMessageContent("hello")}},
	}
	if err := validateChatRequest(invalid); err == nil {
		t.Fatal("expected invalid role error")
	}
}

func TestValidateChatRequestAcceptsDeveloperAndContentParts(t *testing.T) {
	var req backends.ChatRequest
	if err := json.Unmarshal([]byte(`{
		"model":"llama3:8b",
		"messages":[{"role":"developer","content":[{"type":"text","text":"keep answers short"}]}]
	}`), &req); err != nil {
		t.Fatal(err)
	}
	if err := validateChatRequest(&req); err != nil {
		t.Fatal(err)
	}
	if got := req.Messages[0].Content.String(); got != "keep answers short" {
		t.Fatalf("content = %q", got)
	}
}

func TestValidateSamplingAllowsNForSingleCompletionCompatibility(t *testing.T) {
	n := 2
	req := &backends.ChatRequest{
		Model:    "llama3:8b",
		Messages: []backends.ChatMessage{{Role: "user", Content: backends.NewMessageContent("hello")}},
		N:        &n,
	}
	if err := validateChatRequest(req); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSamplingRejectsOutOfRangeValues(t *testing.T) {
	temperature := 2.5
	topP := 1.1
	for name, req := range map[string]*backends.ChatRequest{
		"temperature": {
			Model:       "llama3:8b",
			Messages:    []backends.ChatMessage{{Role: "user", Content: backends.NewMessageContent("hello")}},
			Temperature: &temperature,
		},
		"top_p": {
			Model:    "llama3:8b",
			Messages: []backends.ChatMessage{{Role: "user", Content: backends.NewMessageContent("hello")}},
			TopP:     &topP,
		},
	} {
		if err := validateChatRequest(req); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestDecodeJSONBodyRejectsTrailingJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"label":"one"} {"label":"two"}`))
	res := httptest.NewRecorder()
	var out struct {
		Label string `json:"label"`
	}
	if err := decodeJSONBody(res, req, &out); err == nil {
		t.Fatal("expected trailing JSON error")
	}
}

func TestRouteErrorCodeUsesSentinels(t *testing.T) {
	if code := routeErrorCode(modelrouter.ErrModelNotRegistered); code != "MODEL_NOT_REGISTERED" {
		t.Fatalf("got %s", code)
	}
	if code := routeErrorCode(errors.New("other")); code != "NO_MODEL_FITS" {
		t.Fatalf("got %s", code)
	}
}

func TestCompletionFinishReasonUsesLengthAtMaxTokens(t *testing.T) {
	maxTokens := 4
	if got := completionFinishReason(&maxTokens, 4); got != "length" {
		t.Fatalf("finish reason = %q", got)
	}
	if got := completionFinishReason(&maxTokens, 3); got != "stop" {
		t.Fatalf("finish reason = %q", got)
	}
}
