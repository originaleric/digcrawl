package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/originaleric/digcrawl/protocol"
)

// DeliveryTimeout bounds result delivery after tool execution has finished.
const DeliveryTimeout = 120 * time.Second

// Materialize makes the private Bridge response independent of future RPCs.
// These values belong to this result, not a cache or another resource owner.
func Materialize(ctx context.Context, items []protocol.Artifact, store Store, limit int64) []protocol.Artifact {
	result := append([]protocol.Artifact(nil), items...)
	for i := range result {
		data, mime, err := ReadContent(ctx, result[i], store, limit)
		if err != nil {
			result[i].InlineError = err.Error()
			result[i].InlineData = nil
			continue
		}
		if data == nil {
			data = []byte{}
		}
		result[i].InlineData = &data
		result[i].InlineError = ""
		result[i].Type = mime
	}
	return result
}

// ReadContent uses a call's delivered bytes when present. In-process runtimes
// still read their own store; an explicitly failed delivery never falls back
// to another RPC. Check the actual size as well as the reference's size.
func ReadContent(ctx context.Context, item protocol.Artifact, store Store, configuredLimit int64) ([]byte, string, error) {
	limit := InlineLimit(configuredLimit)
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if item.Size > limit {
		return nil, "", fmt.Errorf("artifact %q exceeds %d byte inline limit for this MCP transport", item.ID, limit)
	}
	if item.InlineError != "" {
		return nil, "", errors.New(item.InlineError)
	}
	var data []byte
	mime := item.Type
	var err error
	switch {
	case item.InlineData != nil:
		data = *item.InlineData
	case store == nil:
		return nil, "", fmt.Errorf("artifact store is unavailable")
	default:
		if files, ok := store.(FileStore); ok {
			var reader io.ReadCloser
			reader, mime, err = files.Open(ctx, item.ID)
			if err == nil {
				// Avoid overflow when a caller explicitly configures MaxInt64.
				readLimit := limit
				if readLimit < int64(^uint64(0)>>1) {
					readLimit++
				}
				data, err = io.ReadAll(io.LimitReader(reader, readLimit))
				_ = reader.Close()
			}
		} else {
			data, mime, err = store.Get(ctx, item.ID)
		}
	}
	if err != nil {
		return nil, "", fmt.Errorf("artifact %q is unavailable: %w", item.ID, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", fmt.Errorf("artifact %q exceeds %d byte inline limit for this MCP transport", item.ID, limit)
	}
	return data, mime, nil
}
