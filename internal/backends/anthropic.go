package backends

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultAnthropicVersion = "2023-06-01"
const defaultAnthropicMaxTokens = 4096

// AnthropicBackend translates OpenAI requests to Anthropic's Messages API.
type AnthropicBackend struct {
	apiKey             string
	baseURL            string
	anthropicVersion   string
	client             *http.Client
}

// NewAnthropicBackend creates a backend targeting the Anthropic API.
func NewAnthropicBackend(apiKey, baseURL, apiVersion string) (*AnthropicBackend, error) {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	if apiVersion == "" {
		apiVersion = defaultAnthropicVersion
	}
	if err := ValidateBackendURL(baseURL); err != nil {
		return nil, fmt.Errorf("anthropic backend: %w", err)
	}
	return &AnthropicBackend{
		apiKey:           apiKey,
		baseURL:          strings.TrimRight(baseURL, "/"),
		anthropicVersion: apiVersion,
		client:           &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (b *AnthropicBackend) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	b.setHeaders(req)
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("anthropic ping returned %d", resp.StatusCode)
	}
	return nil
}

func (b *AnthropicBackend) Chat(ctx context.Context, req *ChatRequest, stream bool) (*ChatResponse, error) {
	anthropicReq := b.toAnthropicRequest(req)
	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	b.setHeaders(httpReq)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("upstream error %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Model      string `json:"model"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	content := ""
	for _, block := range result.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}
	return &ChatResponse{
		Model:        result.Model,
		Content:      content,
		FinishReason: translateAnthropicStopReason(result.StopReason),
		Usage: Usage{
			PromptTokens:     result.Usage.InputTokens,
			CompletionTokens: result.Usage.OutputTokens,
			TotalTokens:      result.Usage.InputTokens + result.Usage.OutputTokens,
		},
	}, nil
}

func (b *AnthropicBackend) StreamChat(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error {
	anthropicReq := b.toAnthropicRequest(req)
	anthropicReq["stream"] = true
	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	b.setHeaders(httpReq)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("upstream error %d: %s", resp.StatusCode, string(errBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var event map[string]json.RawMessage
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		typeBytes, ok := event["type"]
		if !ok {
			continue
		}
		var eventType string
		if err := json.Unmarshal(typeBytes, &eventType); err != nil {
			continue
		}
		switch eventType {
		case "content_block_delta":
			var delta struct {
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err == nil && delta.Delta.Type == "text_delta" {
				if delta.Delta.Text != "" {
					ch <- StreamChunk{Content: delta.Delta.Text}
				}
			}
		case "message_delta":
			var md struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &md); err == nil {
				ch <- StreamChunk{
					FinishReason: translateAnthropicStopReason(md.Delta.StopReason),
					Usage:        Usage{CompletionTokens: md.Usage.OutputTokens},
				}
			}
		}
	}
	return scanner.Err()
}

func (b *AnthropicBackend) toAnthropicRequest(req *ChatRequest) map[string]interface{} {
	var systemParts []string
	var messages []map[string]string
	for _, msg := range req.Messages {
		role := msg.Role
		content := msg.Content.String()
		if role == "system" || role == "developer" {
			systemParts = append(systemParts, content)
			continue
		}
		if role == "assistant" {
			role = "assistant"
		} else {
			role = "user"
		}
		messages = append(messages, map[string]string{"role": role, "content": content})
	}

	maxTokens := defaultAnthropicMaxTokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}

	result := map[string]interface{}{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": maxTokens,
	}
	if len(systemParts) > 0 {
		result["system"] = strings.Join(systemParts, "\n\n")
	}
	if req.Temperature != nil {
		result["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		result["top_p"] = *req.TopP
	}
	stopSeqs := req.Stop.Values()
	if len(stopSeqs) > 0 {
		result["stop_sequences"] = stopSeqs
	}
	return result
}

func (b *AnthropicBackend) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", b.apiKey)
	req.Header.Set("anthropic-version", b.anthropicVersion)
	stripAegisHeaders(map[string][]string(req.Header))
}

func translateAnthropicStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	default:
		return "stop"
	}
}
