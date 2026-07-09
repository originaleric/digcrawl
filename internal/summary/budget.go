package summary

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

func ApplyBudget(req Request, b InputBudget) Request {
	if b.TextChars <= 0 {
		b.TextChars = 12000
	}
	if b.ASRChars <= 0 {
		b.ASRChars = 12000
	}
	if b.OCRChars <= 0 {
		b.OCRChars = 4000
	}
	if b.MetadataChars <= 0 {
		b.MetadataChars = 2000
	}
	if b.MaxInputChars <= 0 {
		b.MaxInputChars = 30000
	}
	out := req
	out.Items = make([]Item, 0, len(req.Items))
	for _, item := range req.Items {
		item.Text = smartTrim(item.Text, b.TextChars)
		item.ASRText = smartTrim(item.ASRText, b.ASRChars)
		item.OCRText = compactLines(item.OCRText, b.OCRChars)
		item.Metadata = trimMetadata(item.Metadata, b.MetadataChars)
		out.Items = append(out.Items, item)
	}
	for encodedLen(out) > b.MaxInputChars && len(out.Items) > 0 {
		last := len(out.Items) - 1
		out.Items[last].Text = halve(out.Items[last].Text)
		out.Items[last].ASRText = halve(out.Items[last].ASRText)
		out.Items[last].OCRText = halve(out.Items[last].OCRText)
		if out.Items[last].Text == "" && out.Items[last].ASRText == "" && out.Items[last].OCRText == "" {
			break
		}
	}
	return out
}

func smartTrim(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	head := max * 3 / 5
	tail := max - head
	if head < 0 {
		head = 0
	}
	if tail < 0 {
		tail = 0
	}
	return strings.TrimSpace(string(r[:head])) + "\n...\n" + strings.TrimSpace(string(r[len(r)-tail:]))
}

func compactLines(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) <= max {
		return s
	}
	seen := map[string]bool{}
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
	}
	return smartTrim(strings.Join(lines, "\n"), max)
}

func trimMetadata(m map[string]any, max int) map[string]any {
	if len(m) == 0 {
		return nil
	}
	allowed := map[string]bool{
		"likes": true, "comments": true, "shares": true, "reposts": true,
		"bookmarks": true, "read_channel": true, "extraction_source": true,
		"ocr_count": true, "asr_provider": true, "duration": true,
	}
	out := map[string]any{}
	for k, v := range m {
		if allowed[k] {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	data, err := json.Marshal(out)
	if err != nil || len(data) <= max {
		return out
	}
	return map[string]any{"preview": smartTrim(string(data), max)}
}

func encodedLen(req Request) int {
	data, err := json.Marshal(req)
	if err != nil {
		return 0
	}
	return len(data)
}

func halve(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= 1 {
		return ""
	}
	return string(r[:len(r)/2])
}
