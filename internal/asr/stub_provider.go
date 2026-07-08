package asr

import (
	"context"
	"os"
)

type stubProvider struct {
	model string
}

func (p stubProvider) Name() string  { return "stub" }
func (p stubProvider) Model() string { return p.model }

func (p stubProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	_ = ctx
	info, _ := os.Stat(audioPath)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return &Result{
		Text:  "stub ASR transcript",
		Audio: AudioInfo{SizeBytes: size},
	}, nil
}
