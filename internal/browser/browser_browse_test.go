package browser

import (
	"strings"
	"testing"

	"github.com/go-rod/rod/lib/proto"
)

func TestMediaResourceProbeScriptUsesOnlyPlayableHTTPResources(t *testing.T) {
	t.Parallel()
	for _, required := range []string{
		"const primaryPlayer",
		"player.click()",
		"await video.play()",
		"getBoundingClientRect()",
		"A URL sourced directly from a <video> element",
	} {
		if !strings.Contains(mediaResourceProbeScript, required) {
			t.Fatalf("media probe missing %q", required)
		}
	}
	if strings.Contains(mediaResourceProbeScript, "blob:") {
		t.Fatal("media probe must not return browser-local blob URLs")
	}
}

func TestIsVideoMediaResponse(t *testing.T) {
	t.Parallel()
	for _, event := range []*proto.NetworkResponseReceived{
		{Type: proto.NetworkResourceTypeMedia, Response: &proto.NetworkResponse{URL: "https://cdn.example/video"}},
		{Response: &proto.NetworkResponse{URL: "https://cdn.example/master", MIMEType: "application/vnd.apple.mpegurl"}},
		{Response: &proto.NetworkResponse{URL: "https://cdn.example/video", MIMEType: "video/mp4"}},
	} {
		if !isVideoMediaResponse(event) {
			t.Fatalf("expected media response: %#v", event)
		}
	}
	if isVideoMediaResponse(&proto.NetworkResponseReceived{Response: &proto.NetworkResponse{URL: "https://cdn.example/image", MIMEType: "image/jpeg"}}) {
		t.Fatal("image response must not be treated as video media")
	}
}
