package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type apiProvider struct {
	apiKey         string
	baseURL        string
	model          string
	language       string
	prompt         string
	responseFormat string
	timeout        time.Duration
	maxAudioBytes  int64
}

func (p *apiProvider) Name() string  { return "api" }
func (p *apiProvider) Model() string { return p.model }

func (p *apiProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	info, err := os.Stat(audioPath)
	if err != nil {
		return nil, fmt.Errorf("stat audio: %w", err)
	}
	if p.maxAudioBytes > 0 && info.Size() > p.maxAudioBytes {
		return nil, fmt.Errorf("audio payload exceeds MaxAudioBytes (%d > %d)", info.Size(), p.maxAudioBytes)
	}
	file, err := os.Open(audioPath)
	if err != nil {
		return nil, fmt.Errorf("open audio: %w", err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	_ = writer.WriteField("model", p.model)
	if strings.TrimSpace(p.responseFormat) != "" {
		_ = writer.WriteField("response_format", strings.TrimSpace(p.responseFormat))
	}
	if p.language != "" {
		_ = writer.WriteField("language", p.language)
	}
	if p.prompt != "" {
		_ = writer.WriteField("prompt", p.prompt)
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.baseURL, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ASR API status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	format := strings.ToLower(strings.TrimSpace(p.responseFormat))
	if format == "" {
		format = "verbose_json"
	}
	switch format {
	case "text", "srt", "vtt":
		return &Result{
			Text:  strings.TrimSpace(string(data)),
			Audio: AudioInfo{SizeBytes: info.Size()},
		}, nil
	case "json", "verbose_json":
	default:
		return nil, fmt.Errorf("unsupported ASR response format %q", p.responseFormat)
	}

	var raw struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode ASR API response: %w", err)
	}
	result := &Result{
		Text:     strings.TrimSpace(raw.Text),
		Language: raw.Language,
		Duration: raw.Duration,
		Audio:    AudioInfo{SizeBytes: info.Size()},
	}
	for _, seg := range raw.Segments {
		result.Segments = append(result.Segments, Segment{
			Start: seg.Start,
			End:   seg.End,
			Text:  strings.TrimSpace(seg.Text),
		})
	}
	return result, nil
}
