package backends

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMessageContentUnmarshalStringAndParts(t *testing.T) {
	var text MessageContent
	if err := json.Unmarshal([]byte(`"hello"`), &text); err != nil {
		t.Fatal(err)
	}
	if got := text.String(); got != "hello" {
		t.Fatalf("string content = %q", got)
	}

	var parts MessageContent
	if err := json.Unmarshal([]byte(`[{"type":"text","text":"first"},{"type":"image_url","image_url":{"url":"data:"}},{"type":"text","text":"second"}]`), &parts); err != nil {
		t.Fatal(err)
	}
	if got := parts.String(); got != "first\nsecond" {
		t.Fatalf("part content = %q", got)
	}
}

func TestMessageContentPreservesImageParts(t *testing.T) {
	var content MessageContent
	raw := `[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}},{"type":"text","text":"look"}]`
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatal(err)
	}
	if !content.HasImages() || content.String() != "look" {
		t.Fatalf("unexpected content: %#v", content.Parts())
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "image_url") {
		t.Fatalf("image part was lost: %s", encoded)
	}
}

func TestCompletionPromptUnmarshalStringArray(t *testing.T) {
	var prompt CompletionPrompt
	if err := json.Unmarshal([]byte(`["first","second"]`), &prompt); err != nil {
		t.Fatal(err)
	}
	if got := prompt.String(); got != "first\nsecond" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestStopSequencesUnmarshalStringAndArray(t *testing.T) {
	var single StopSequences
	if err := json.Unmarshal([]byte(`"END"`), &single); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(single.Values(), []string{"END"}) {
		t.Fatalf("single stop = %#v", single.Values())
	}

	var many StopSequences
	if err := json.Unmarshal([]byte(`["END","STOP"]`), &many); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(many.Values(), []string{"END", "STOP"}) {
		t.Fatalf("many stop = %#v", many.Values())
	}
}

func TestWireMessagesNormalizeForLocalBackends(t *testing.T) {
	messages := wireMessages([]ChatMessage{{
		Role:    "developer",
		Content: NewMessageContent("rules"),
	}})
	if len(messages) != 1 {
		t.Fatalf("message count = %d", len(messages))
	}
	if messages[0].Role != "system" || messages[0].Content != "rules" {
		t.Fatalf("wire message = %#v", messages[0])
	}
}
