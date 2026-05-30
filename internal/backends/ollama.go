package backends

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaBackend translates Aegis requests to the Ollama HTTP API.
type OllamaBackend struct {
	baseURL string
	client  *http.Client
}

// NewOllamaBackend creates an Ollama backend.
func NewOllamaBackend(baseURL string) *OllamaBackend {
	return &OllamaBackend{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 0},
	}
}

// Ping checks whether Ollama is reachable.
func (b *OllamaBackend) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("ollama ping failed with status %d", res.StatusCode)
	}
	return nil
}

// Chat sends a non-streaming chat request to Ollama.
func (b *OllamaBackend) Chat(ctx context.Context, req *ChatRequest, stream bool) (*ChatResponse, error) {
	body := ollamaChatRequest{
		Model:    req.Model,
		Messages: wireMessages(req.Messages),
		Stream:   false,
		Options:  ollamaOptions{Temperature: req.Temperature, TopP: req.TopP, NumPredict: req.MaxTokens, Stop: req.Stop.Values(), Seed: req.Seed},
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/chat", &buf)
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
		return nil, backendStatusError("ollama chat", res)
	}
	var out ollamaChatResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &ChatResponse{
		Model:        req.Model,
		Content:      out.Message.Content,
		FinishReason: out.DoneReason,
		Usage: Usage{
			PromptTokens:     out.PromptEvalCount,
			CompletionTokens: out.EvalCount,
			TotalTokens:      out.PromptEvalCount + out.EvalCount,
		},
	}, nil
}

// StreamChat streams token text from Ollama into ch.
func (b *OllamaBackend) StreamChat(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error {
	body := ollamaChatRequest{
		Model:    req.Model,
		Messages: wireMessages(req.Messages),
		Stream:   true,
		Options:  ollamaOptions{Temperature: req.Temperature, TopP: req.TopP, NumPredict: req.MaxTokens, Stop: req.Stop.Values(), Seed: req.Seed},
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/chat", &buf)
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
		return backendStatusError("ollama stream", res)
	}

	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var chunk ollamaChatResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			return err
		}
		if chunk.Message.Content != "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ch <- StreamChunk{Content: chunk.Message.Content}:
			}
		}
		if chunk.Done {
			if chunk.DoneReason != "" || chunk.PromptEvalCount > 0 || chunk.EvalCount > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case ch <- StreamChunk{
					FinishReason: chunk.DoneReason,
					Usage: Usage{
						PromptTokens:     chunk.PromptEvalCount,
						CompletionTokens: chunk.EvalCount,
						TotalTokens:      chunk.PromptEvalCount + chunk.EvalCount,
					},
				}:
				}
			}
			return nil
		}
	}
	return scanner.Err()
}

type ollamaChatRequest struct {
	Model    string            `json:"model"`
	Messages []wireChatMessage `json:"messages"`
	Stream   bool              `json:"stream"`
	Options  ollamaOptions     `json:"options,omitempty"`
}

type ollamaOptions struct {
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	NumPredict  *int     `json:"num_predict,omitempty"`
	Stop        []string `json:"stop,omitempty"`
	Seed        *int     `json:"seed,omitempty"`
}

type ollamaChatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

func backendStatusError(operation string, res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	return fmt.Errorf("%s failed with status %d: %s", operation, res.StatusCode, string(body))
}
