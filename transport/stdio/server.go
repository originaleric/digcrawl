package stdiogw

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime"
)

// Server handles newline-delimited JSON gateway protocol on stdin/stdout.
type Server struct {
	rt     *runtime.Runtime
	reader *bufio.Reader
	writer io.Writer
}

// NewServer creates a stdio gateway server using os.Stdin/os.Stdout.
func NewServer(rt *runtime.Runtime) *Server {
	return NewServerWithIO(rt, os.Stdin, os.Stdout)
}

// NewServerWithIO creates a stdio gateway server with custom IO (for tests).
func NewServerWithIO(rt *runtime.Runtime, r io.Reader, w io.Writer) *Server {
	return &Server{
		rt:     rt,
		reader: bufio.NewReader(r),
		writer: w,
	}
}

// Run processes messages until EOF or context cancel.
func (s *Server) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = trimLine(line)
		if len(line) == 0 {
			continue
		}
		resp, err := s.handleLine(ctx, line)
		if err != nil {
			wire := protocol.NewWireError("INVALID_REQUEST", err.Error())
			resp, _ = wire.Encode()
		}
		if _, err := s.writer.Write(append(resp, '\n')); err != nil {
			return err
		}
	}
}

func (s *Server) handleLine(ctx context.Context, line []byte) ([]byte, error) {
	var peek struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &peek); err != nil {
		return nil, err
	}
	switch peek.Type {
	case protocol.TypeGetManifest, protocol.TypeToolManifest:
		return json.Marshal(s.rt.Manifest())
	case protocol.TypeToolCall:
		env, err := protocol.DecodeEnvelope(line)
		if err != nil {
			return nil, err
		}
		if env.ToolCall == nil {
			return nil, fmt.Errorf("missing tool_call body")
		}
		return json.Marshal(s.rt.Execute(ctx, env.ToolCall))
	}
	if peek.Type != "" {
		return nil, fmt.Errorf("unknown message type %q", peek.Type)
	}
	var call protocol.ToolCall
	if err := json.Unmarshal(line, &call); err != nil {
		return nil, err
	}
	if call.Tool == "" {
		return nil, fmt.Errorf("missing tool name")
	}
	if call.Type == "" {
		call.Type = protocol.TypeToolCall
	}
	return json.Marshal(s.rt.Execute(ctx, &call))
}

func trimLine(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
