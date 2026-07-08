package asr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type localProvider struct {
	command  string
	args     []string
	model    string
	language string
	timeout  time.Duration
}

func (p *localProvider) Name() string  { return "local" }
func (p *localProvider) Model() string { return p.model }

func (p *localProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	info, err := os.Stat(audioPath)
	if err != nil {
		return nil, fmt.Errorf("stat audio: %w", err)
	}
	if _, err := exec.LookPath(p.command); err != nil {
		return nil, fmt.Errorf("local ASR command %q not found in PATH; install it or configure ASR.LocalCommand / --local-command", p.command)
	}
	outDir, err := os.MkdirTemp("", "digcrawl-asr-local-*")
	if err != nil {
		return nil, fmt.Errorf("create local ASR temp dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	args := p.args
	if len(args) == 0 {
		args = []string{"{audio_path}", "--model", "{model}", "--output_format", "json", "--output_dir", "{output_dir}"}
		if p.language != "" {
			args = append(args, "--language", "{language}")
		}
	}
	args = expandArgs(args, map[string]string{
		"audio_path": audioPath,
		"model":      p.model,
		"language":   p.language,
		"output_dir": outDir,
	})

	cmdCtx := ctx
	cancel := func() {}
	if p.timeout > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, p.timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, p.command, args...)
	stdout, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("local ASR failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("local ASR failed: %w", err)
	}

	result, parseErr := parseTranscriptJSON(stdout)
	if parseErr != nil || strings.TrimSpace(result.Text) == "" {
		result, parseErr = readTranscriptFromDir(outDir)
	}
	if parseErr != nil {
		text := strings.TrimSpace(string(stdout))
		if text == "" {
			return nil, parseErr
		}
		result = &Result{Text: text}
	}
	result.Audio.SizeBytes = info.Size()
	return result, nil
}

func expandArgs(args []string, values map[string]string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		for key, val := range values {
			arg = strings.ReplaceAll(arg, "{"+key+"}", val)
		}
		if strings.TrimSpace(arg) != "" {
			out = append(out, arg)
		}
	}
	return out
}

func readTranscriptFromDir(dir string) (*Result, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("local ASR produced no JSON transcript")
	}
	sort.Strings(matches)
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, err
	}
	return parseTranscriptJSON(data)
}

func parseTranscriptJSON(data []byte) (*Result, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return &Result{}, fmt.Errorf("empty transcript JSON")
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
		return &Result{}, err
	}
	result := &Result{
		Text:     strings.TrimSpace(raw.Text),
		Language: raw.Language,
		Duration: raw.Duration,
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
