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
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"prompt_eval_count":4,"eval_count":2}`))
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
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"lo"},"done":true}`)
	}))
	defer server.Close()

	ch := make(chan string)
	errCh := make(chan error, 1)
	go func() {
		errCh <- NewOllamaBackend(server.URL).StreamChat(context.Background(), &ChatRequest{
			Model:    "llama3:8b",
			Messages: []ChatMessage{{Role: "user", Content: NewMessageContent("hello")}},
		}, ch)
		close(ch)
	}()

	var tokens []string
	for token := range ch {
		tokens = append(tokens, token)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"hel", "lo"}) {
		t.Fatalf("tokens = %#v", tokens)
	}
}
