package youtube

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const captionTimeout = 8 * time.Second
const maxCaptionTracks = 3

var youtubeHTTPClient = &http.Client{
	Timeout: captionTimeout,
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if req.URL != nil && isYouTubeHost(strings.ToLower(req.URL.Hostname())) {
			return nil
		}
		return http.ErrUseLastResponse
	},
}

type captionTrack struct {
	BaseURL      string
	LanguageCode string
	Name         string
	Kind         string
}

func captionTracksFromMeta(meta map[string]any) []captionTrack {
	value, ok := meta["caption_tracks"]
	if !ok || value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	tracks := make([]captionTrack, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		track := captionTrack{
			BaseURL:      strings.TrimSpace(fmt.Sprint(m["base_url"])),
			LanguageCode: strings.TrimSpace(fmt.Sprint(m["language_code"])),
			Name:         strings.TrimSpace(fmt.Sprint(m["name"])),
			Kind:         strings.TrimSpace(fmt.Sprint(m["kind"])),
		}
		if isValidCaptionURL(track.BaseURL) {
			tracks = append(tracks, track)
			if len(tracks) == maxCaptionTracks {
				break
			}
		}
	}
	return tracks
}

func isValidCaptionURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && u.Scheme == "https" && isYouTubeHost(strings.ToLower(u.Hostname()))
}

func fetchFirstCaption(ctx context.Context, tracks []captionTrack) (text string, track captionTrack, err error) {
	// Captions enrich an otherwise complete video read. All track attempts share
	// one budget so an unavailable caption endpoint cannot multiply read latency.
	ctx, cancel := context.WithTimeout(ctx, captionTimeout)
	defer cancel()
	for _, candidate := range tracks {
		text, err = fetchCaption(ctx, candidate.BaseURL)
		if err == nil && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text), candidate, nil
		}
	}
	if err == nil {
		err = fmt.Errorf("no readable YouTube caption track")
	}
	return "", captionTrack{}, err
}

func fetchCaption(ctx context.Context, rawURL string) (string, error) {
	if !isValidCaptionURL(rawURL) {
		return "", fmt.Errorf("caption URL is not an approved YouTube HTTPS URL")
	}
	ctx, cancel := context.WithTimeout(ctx, captionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/xml,text/xml;q=0.9,*/*;q=0.1")
	resp, err := youtubeHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("caption endpoint returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return parseCaptionXML(body)
}

func parseCaptionXML(data []byte) (string, error) {
	var transcript struct {
		Texts []struct {
			Value string `xml:",chardata"`
		} `xml:"text"`
	}
	if err := xml.Unmarshal(data, &transcript); err != nil {
		return "", fmt.Errorf("decode YouTube captions: %w", err)
	}
	lines := make([]string, 0, len(transcript.Texts))
	for _, line := range transcript.Texts {
		if value := strings.Join(strings.Fields(strings.TrimSpace(line.Value)), " "); value != "" {
			lines = append(lines, value)
		}
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("YouTube captions are empty")
	}
	return strings.Join(lines, "\n"), nil
}
