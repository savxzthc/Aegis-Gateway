package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Backend defines the minimal local model runner interface.
type Backend interface {
	Chat(ctx context.Context, req *ChatRequest, stream bool) (*ChatResponse, error)
	StreamChat(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error
	Ping(ctx context.Context) error
}

// EmbeddingBackend is an optional interface for backends that support embeddings.
type EmbeddingBackend interface {
	Backend
	Embed(ctx context.Context, model string, inputs []string) ([][]float64, error)
}

// ChatMessage is an OpenAI-compatible chat message.
type ChatMessage struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
	Name    string         `json:"name,omitempty"`
}

// MessageContent preserves ordered OpenAI text and image content parts.
type MessageContent struct {
	parts []ChatContentPart
}

// ChatContentPart is a supported OpenAI chat content part.
type ChatContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL is an OpenAI-compatible image reference.
type ImageURL struct {
	URL string `json:"url"`
}

// NewMessageContent creates text chat content.
func NewMessageContent(text string) MessageContent {
	return MessageContent{parts: []ChatContentPart{{Type: "text", Text: text}}}
}

// NewMessageParts creates structured multimodal message content.
func NewMessageParts(parts []ChatContentPart) MessageContent {
	return MessageContent{parts: append([]ChatContentPart(nil), parts...)}
}

// String returns normalized message text.
func (c MessageContent) String() string {
	texts := make([]string, 0, len(c.parts))
	for _, part := range c.parts {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// Parts returns a defensive copy of the ordered content parts.
func (c MessageContent) Parts() []ChatContentPart {
	return append([]ChatContentPart(nil), c.parts...)
}

// HasImages reports whether the message includes image content.
func (c MessageContent) HasImages() bool {
	for _, part := range c.parts {
		if part.Type == "image_url" && part.ImageURL != nil && strings.TrimSpace(part.ImageURL.URL) != "" {
			return true
		}
	}
	return false
}

// Empty reports whether the content has no non-space text.
func (c MessageContent) Empty() bool {
	return strings.TrimSpace(c.String()) == "" && !c.HasImages()
}

// MarshalJSON uses the compact string form for text-only content.
func (c MessageContent) MarshalJSON() ([]byte, error) {
	if !c.HasImages() {
		return json.Marshal(c.String())
	}
	return json.Marshal(c.parts)
}

// UnmarshalJSON decodes string or ordered text/image part content.
func (c *MessageContent) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		c.parts = nil
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		c.parts = []ChatContentPart{{Type: "text", Text: text}}
		return nil
	}
	var parts []ChatContentPart
	if err := json.Unmarshal(data, &parts); err == nil {
		for i, part := range parts {
			switch part.Type {
			case "text":
			case "image_url":
				if part.ImageURL == nil || strings.TrimSpace(part.ImageURL.URL) == "" {
					return fmt.Errorf("message content part %d image_url.url is required", i)
				}
			default:
				return fmt.Errorf("message content part %d has unsupported type %q", i, part.Type)
			}
		}
		c.parts = append([]ChatContentPart(nil), parts...)
		return nil
	}
	return fmt.Errorf("message content must be a string or an array of text/image parts")
}

// ChatRequest is the internal typed chat completion request.
type ChatRequest struct {
	Model            string        `json:"model"`
	Messages         []ChatMessage `json:"messages"`
	Stream           bool          `json:"stream,omitempty"`
	MaxTokens        *int          `json:"max_tokens,omitempty"`
	Temperature      *float64      `json:"temperature,omitempty"`
	TopP             *float64      `json:"top_p,omitempty"`
	Stop             StopSequences `json:"stop,omitempty"`
	Seed             *int          `json:"seed,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
	N                *int          `json:"n,omitempty"`
	User             string        `json:"user,omitempty"`
	StreamOptions    StreamOptions `json:"stream_options,omitempty"`
	ConversationID   string        `json:"conversation_id,omitempty"`
	Persist          bool          `json:"persist,omitempty"`
	Search           *bool         `json:"search,omitempty"`
}

// StreamOptions contains OpenAI-compatible streaming options.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type upstreamChatRequest struct {
	Model            string        `json:"model"`
	Messages         []ChatMessage `json:"messages"`
	Stream           bool          `json:"stream,omitempty"`
	MaxTokens        *int          `json:"max_tokens,omitempty"`
	Temperature      *float64      `json:"temperature,omitempty"`
	TopP             *float64      `json:"top_p,omitempty"`
	Stop             StopSequences `json:"stop,omitempty"`
	Seed             *int          `json:"seed,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
	N                *int          `json:"n,omitempty"`
	User             string        `json:"user,omitempty"`
	StreamOptions    StreamOptions `json:"stream_options,omitempty"`
}

func upstreamRequest(req *ChatRequest) upstreamChatRequest {
	return upstreamChatRequest{
		Model: req.Model, Messages: req.Messages, Stream: req.Stream, MaxTokens: req.MaxTokens,
		Temperature: req.Temperature, TopP: req.TopP, Stop: req.Stop, Seed: req.Seed,
		PresencePenalty: req.PresencePenalty, FrequencyPenalty: req.FrequencyPenalty,
		N: req.N, User: req.User, StreamOptions: req.StreamOptions,
	}
}

// CompletionRequest is the typed legacy completion request.
type CompletionRequest struct {
	Model            string           `json:"model"`
	Prompt           CompletionPrompt `json:"prompt"`
	Stream           bool             `json:"stream,omitempty"`
	MaxTokens        *int             `json:"max_tokens,omitempty"`
	Temperature      *float64         `json:"temperature,omitempty"`
	TopP             *float64         `json:"top_p,omitempty"`
	Stop             StopSequences    `json:"stop,omitempty"`
	Seed             *int             `json:"seed,omitempty"`
	PresencePenalty  *float64         `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64         `json:"frequency_penalty,omitempty"`
	N                *int             `json:"n,omitempty"`
	User             string           `json:"user,omitempty"`
	ConversationID   string           `json:"conversation_id,omitempty"`
	Persist          bool             `json:"persist,omitempty"`
	Search           *bool            `json:"search,omitempty"`
}

// Usage contains approximate token usage reported by a backend.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse contains normalized backend output.
type ChatResponse struct {
	Model        string `json:"model"`
	Content      string `json:"content"`
	Usage        Usage  `json:"usage"`
	FinishReason string `json:"finish_reason"`
}

// StreamChunk contains one normalized backend stream event.
type StreamChunk struct {
	Content      string
	FinishReason string
	Usage        Usage
}

// CompletionPrompt stores normalized text from OpenAI legacy prompt input.
type CompletionPrompt struct {
	values []string
}

// String joins one or more prompt strings for local backends.
func (p CompletionPrompt) String() string {
	return strings.Join(p.values, "\n")
}

// Empty reports whether the prompt has no non-space text.
func (p CompletionPrompt) Empty() bool {
	return strings.TrimSpace(p.String()) == ""
}

// MarshalJSON encodes prompt input using the compact OpenAI shape.
func (p CompletionPrompt) MarshalJSON() ([]byte, error) {
	if len(p.values) == 1 {
		return json.Marshal(p.values[0])
	}
	return json.Marshal(p.values)
}

// UnmarshalJSON decodes string or string-array prompt input.
func (p *CompletionPrompt) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		p.values = nil
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		p.values = []string{text}
		return nil
	}
	var texts []string
	if err := json.Unmarshal(data, &texts); err == nil {
		p.values = texts
		return nil
	}
	return fmt.Errorf("prompt must be a string or an array of strings")
}

// StopSequences stores one or more completion stop sequences.
type StopSequences []string

// Values returns a defensive copy of configured stop sequences.
func (s StopSequences) Values() []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for _, value := range s {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// MarshalJSON encodes stop as a string for one value or an array for many.
func (s StopSequences) MarshalJSON() ([]byte, error) {
	values := s.Values()
	if len(values) == 1 {
		return json.Marshal(values[0])
	}
	return json.Marshal(values)
}

// UnmarshalJSON decodes string or string-array stop input.
func (s *StopSequences) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*s = nil
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = StopSequences{text}
		return nil
	}
	var texts []string
	if err := json.Unmarshal(data, &texts); err == nil {
		*s = StopSequences(texts)
		return nil
	}
	return fmt.Errorf("stop must be a string or an array of strings")
}

type wireChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func wireMessages(messages []ChatMessage) []wireChatMessage {
	out := make([]wireChatMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, wireChatMessage{
			Role:    backendRole(message.Role),
			Content: message.Content.String(),
		})
	}
	return out
}

type structuredWireChatMessage struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
}

func structuredWireMessages(messages []ChatMessage) []structuredWireChatMessage {
	out := make([]structuredWireChatMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, structuredWireChatMessage{Role: backendRole(message.Role), Content: message.Content})
	}
	return out
}

func backendRole(role string) string {
	if role == "developer" {
		return "system"
	}
	return role
}
