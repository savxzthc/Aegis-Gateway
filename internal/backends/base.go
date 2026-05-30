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

// ChatMessage is an OpenAI-compatible chat message.
type ChatMessage struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
	Name    string         `json:"name,omitempty"`
}

// MessageContent stores normalized text from OpenAI chat message content.
type MessageContent struct {
	text string
}

// ChatContentPart is a supported OpenAI chat content part.
type ChatContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// NewMessageContent creates text chat content.
func NewMessageContent(text string) MessageContent {
	return MessageContent{text: text}
}

// String returns normalized message text.
func (c MessageContent) String() string {
	return c.text
}

// Empty reports whether the content has no non-space text.
func (c MessageContent) Empty() bool {
	return strings.TrimSpace(c.text) == ""
}

// MarshalJSON encodes message content as an OpenAI-compatible text string.
func (c MessageContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.text)
}

// UnmarshalJSON decodes string or text-part array message content.
func (c *MessageContent) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		c.text = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		c.text = text
		return nil
	}
	var parts []ChatContentPart
	if err := json.Unmarshal(data, &parts); err == nil {
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			if part.Type == "text" && part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		c.text = strings.Join(texts, "\n")
		return nil
	}
	return fmt.Errorf("message content must be a string or an array of text parts")
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
}

// StreamOptions contains OpenAI-compatible streaming options.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
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

func backendRole(role string) string {
	if role == "developer" {
		return "system"
	}
	return role
}
