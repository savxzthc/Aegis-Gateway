package backends

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestLlamaCppBackendChatTranslatesRequest(t *testing.T) {
	var got llamaChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`))
	}))
	defer server.Close()

	maxTokens := 64
	temperature := 0.7
	topP := 0.9
	seed := 42
	resp, err := NewLlamaCppBackend(server.URL).Chat(context.Background(), &ChatRequest{
		Model:       "local",
		Messages:    []ChatMessage{{Role: "developer", Content: NewMessageContent("rules")}},
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
		TopP:        &topP,
		Stop:        StopSequences{"END"},
		Seed:        &seed,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" || resp.Usage.TotalTokens != 6 {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("finish reason = %q", resp.FinishReason)
	}
	if got.Model != "local" || got.Stream {
		t.Fatalf("unexpected request basics: %#v", got)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "system" || got.Messages[0].Content != "rules" {
		t.Fatalf("messages not normalized: %#v", got.Messages)
	}
	if got.MaxTokens == nil || *got.MaxTokens != 64 {
		t.Fatalf("max_tokens not forwarded: %#v", got)
	}
	if got.Seed == nil || *got.Seed != 42 {
		t.Fatalf("seed not forwarded: %#v", got)
	}
	if !reflect.DeepEqual(got.Stop, []string{"END"}) {
		t.Fatalf("stop not forwarded: %#v", got.Stop)
	}
}

func TestLlamaCppBackendStreamChatParsesSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprintln(w, `: keepalive`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hel"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"lo"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"length"}],"usage":{"completion_tokens":2,"total_tokens":2}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: [DONE]`)
		fmt.Fprintln(w)
	}))
	defer server.Close()

	ch := make(chan StreamChunk)
	errCh := make(chan error, 1)
	go func() {
		errCh <- NewLlamaCppBackend(server.URL).StreamChat(context.Background(), &ChatRequest{
			Model:    "local",
			Messages: []ChatMessage{{Role: "user", Content: NewMessageContent("hello")}},
		}, ch)
		close(ch)
	}()

	var tokens []string
	finish := ""
	usage := Usage{}
	for chunk := range ch {
		if chunk.Content != "" {
			tokens = append(tokens, chunk.Content)
		}
		if chunk.FinishReason != "" {
			finish = chunk.FinishReason
		}
		if chunk.Usage.TotalTokens > 0 {
			usage = chunk.Usage
		}
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"hel", "lo"}) {
		t.Fatalf("tokens = %#v", tokens)
	}
	if finish != "length" || usage.CompletionTokens != 2 {
		t.Fatalf("finish=%q usage=%#v", finish, usage)
	}
}
