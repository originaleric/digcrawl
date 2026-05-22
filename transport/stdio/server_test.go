package stdiogw

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/registry"
)

func TestStdioGetManifest(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "browser.browse"},
	})
	rt := runtime.New(reg, runtime.Options{InstanceID: "stdio_test"})
	var in bytes.Buffer
	var out bytes.Buffer
	req, _ := json.Marshal(map[string]string{"type": protocol.TypeGetManifest})
	in.Write(append(req, '\n'))

	s := NewServerWithIO(rt, &in, &out)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var m protocol.ToolManifest
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &m); err != nil {
		t.Fatal(err)
	}
	if m.InstanceID != "stdio_test" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}
