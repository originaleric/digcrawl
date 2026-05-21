package protocol

import "testing"

func TestDecodeToolCallEnvelope(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"type": "tool_call",
		"id": "c1",
		"tool": "browser.browse",
		"input": {"url": "https://example.com"}
	}`)
	env, err := DecodeEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != TypeToolCall || env.ToolCall == nil || env.ToolCall.ID != "c1" {
		t.Fatalf("unexpected env: %+v", env)
	}
}
