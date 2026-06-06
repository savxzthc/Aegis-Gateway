package backends

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// LlamaCppBackend translates Aegis requests to a llama.cpp server.
type LlamaCppBackend struct {
	baseURL string
	client  *http.Client
}

// NewLlamaCppBackend creates a llama.cpp backend.
func NewLlamaCppBackend(baseURL string) *LlamaCppBackend {
	return &LlamaCppBackend{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 0},
	}
}

// Ping checks whether llama.cpp is reachable.
func (b *LlamaCppBackend) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 500 {
		return fmt.Errorf("llama.cpp ping failed with status %d", res.StatusCode)
	}
	return nil
}

// Chat sends a non-streaming chat request to llama.cpp.
func (b *LlamaCppBackend) Chat(ctx context.Context, req *ChatRequest, stream bool) (*ChatResponse, error) {
	body := llamaChatRequest{
		Model:       req.Model,
		Messages:    structuredWireMessages(req.Messages),
		Stream:      false,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.Stop.Values(),
		Seed:        req.Seed,
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/v1/chat/completions", &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := b.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, backendStatusError("llama.cpp chat", res)
	}
	var out llamaChatResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	content := ""
	finishReason := ""
	if len(out.Choices) > 0 {
		content = out.Choices[0].Message.Content
		finishReason = firstNonEmpty(out.Choices[0].FinishReason, out.Choices[0].StopReason)
	}
	finishReason = firstNonEmpty(finishReason, out.StopReason, out.DoneReason)
	return &ChatResponse{Model: req.Model, Content: content, Usage: out.Usage, FinishReason: finishReason}, nil
}

// StreamChat streams token text from llama.cpp into ch.
func (b *LlamaCppBackend) StreamChat(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error {
	body := llamaChatRequest{
		Model:       req.Model,
		Messages:    structuredWireMessages(req.Messages),
		Stream:      true,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.Stop.Values(),
		Seed:        req.Seed,
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/v1/chat/completions", &buf)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := b.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return backendStatusError("llama.cpp stream", res)
	}

	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if line == "[DONE]" {
			return nil
		}
		var chunk llamaStreamResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return err
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		event := StreamChunk{
			Content:      chunk.Choices[0].Delta.Content,
			FinishReason: firstNonEmpty(chunk.Choices[0].FinishReason, chunk.Choices[0].StopReason, chunk.StopReason, chunk.DoneReason),
			Usage:        chunk.Usage,
		}
		if event.Content == "" && event.FinishReason == "" && event.Usage.TotalTokens == 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- event:
		}
	}
	return scanner.Err()
}

type llamaChatRequest struct {
	Model       string                      `json:"model"`
	Messages    []structuredWireChatMessage `json:"messages"`
	Stream      bool                        `json:"stream"`
	MaxTokens   *int                        `json:"max_tokens,omitempty"`
	Temperature *float64                    `json:"temperature,omitempty"`
	TopP        *float64                    `json:"top_p,omitempty"`
	Stop        []string                    `json:"stop,omitempty"`
	Seed        *int                        `json:"seed,omitempty"`
}

type llamaChatResponse struct {
	Choices []struct {
		Message      wireChatMessage `json:"message"`
		FinishReason string          `json:"finish_reason"`
		StopReason   string          `json:"stop_reason"`
	} `json:"choices"`
	StopReason string `json:"stop_reason"`
	DoneReason string `json:"done_reason"`
	Usage      Usage  `json:"usage"`
}

type llamaStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
		StopReason   string `json:"stop_reason"`
	} `json:"choices"`
	StopReason string `json:"stop_reason"`
	DoneReason string `json:"done_reason"`
	Usage      Usage  `json:"usage"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
