package registry

import (
	"context"
	"encoding/json"

	"github.com/originaleric/diggate/protocol"
)

// Handler executes a tool and returns structured output plus optional artifacts.
type Handler func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error)

// Entry binds a tool name to metadata and handler.
type Entry struct {
	Descriptor protocol.ToolDescriptor
	Handler    Handler
}

// Registry holds gateway-exposed tools.
type Registry struct {
	entries map[string]Entry
}

func New() *Registry {
	return &Registry{entries: make(map[string]Entry)}
}

func (r *Registry) Register(entry Entry) {
	r.entries[entry.Descriptor.Name] = entry
}

func (r *Registry) Get(name string) (Entry, bool) {
	e, ok := r.entries[name]
	return e, ok
}

func (r *Registry) List() []protocol.ToolDescriptor {
	out := make([]protocol.ToolDescriptor, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.Descriptor)
	}
	return out
}

func MustSchema(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
