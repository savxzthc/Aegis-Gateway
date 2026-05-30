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

func TestOllamaBackendChatTranslatesRequest(t *testing.T) {
	var got ollamaChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done_reason":"stop","prompt_eval_count":4,"eval_count":2}`))
	}))
	defer server.Close()

	maxTokens := 64
	temperature := 0.7
	topP := 0.9
	seed := 42
	resp, err := NewOllamaBackend(server.URL).Chat(context.Background(), &ChatRequest{
		Model:       "llama3:8b",
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
	if got.Model != "llama3:8b" || got.Stream {
		t.Fatalf("unexpected request basics: %#v", got)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "system" || got.Messages[0].Content != "rules" {
		t.Fatalf("messages not normalized: %#v", got.Messages)
	}
	if got.Options.NumPredict == nil || *got.Options.NumPredict != 64 {
		t.Fatalf("num_predict not forwarded: %#v", got.Options)
	}
	if got.Options.Seed == nil || *got.Options.Seed != 42 {
		t.Fatalf("seed not forwarded: %#v", got.Options)
	}
	if !reflect.DeepEqual(got.Options.Stop, []string{"END"}) {
		t.Fatalf("stop not forwarded: %#v", got.Options.Stop)
	}
}

func TestOllamaBackendStreamChatParsesTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"hel"}}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"lo"},"done":true,"done_reason":"length","prompt_eval_count":4,"eval_count":2}`)
	}))
	defer server.Close()

	ch := make(chan StreamChunk)
	errCh := make(chan error, 1)
	go func() {
		errCh <- NewOllamaBackend(server.URL).StreamChat(context.Background(), &ChatRequest{
			Model:    "llama3:8b",
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
