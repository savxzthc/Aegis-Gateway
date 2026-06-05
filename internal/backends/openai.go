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

// OpenAIBackend forwards chat completions to an OpenAI-compatible endpoint.
type OpenAIBackend struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenAIBackend creates a backend targeting the OpenAI API.
func NewOpenAIBackend(apiKey, baseURL string) (*OpenAIBackend, error) {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if err := ValidateBackendURL(baseURL); err != nil {
		return nil, fmt.Errorf("openai backend: %w", err)
	}
	return &OpenAIBackend{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (b *OpenAIBackend) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("openai ping returned %d", resp.StatusCode)
	}
	return nil
}

func (b *OpenAIBackend) Chat(ctx context.Context, req *ChatRequest, stream bool) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	stripAegisHeaders(map[string][]string(httpReq.Header))

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
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	content := ""
	finishReason := ""
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
		finishReason = result.Choices[0].FinishReason
	}
	return &ChatResponse{
		Model:        result.Model,
		Content:      content,
		Usage:        result.Usage,
		FinishReason: finishReason,
	}, nil
}

func (b *OpenAIBackend) StreamChat(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	stripAegisHeaders(map[string][]string(httpReq.Header))

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
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta        struct{ Content string `json:"content"` } `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *Usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		sc := StreamChunk{}
		if len(chunk.Choices) > 0 {
			sc.Content = chunk.Choices[0].Delta.Content
			if chunk.Choices[0].FinishReason != nil {
				sc.FinishReason = *chunk.Choices[0].FinishReason
			}
		}
		if chunk.Usage != nil {
			sc.Usage = *chunk.Usage
		}
		if sc.Content != "" || sc.FinishReason != "" {
			ch <- sc
		}
	}
	return scanner.Err()
}
