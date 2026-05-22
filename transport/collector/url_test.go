package collector

import "testing"

func TestBuildWSURL(t *testing.T) {
	t.Parallel()
	u, hdr, err := buildWSURL("https://example.com", "/digcrawl/v1/collector/ws", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if u != "wss://example.com/digcrawl/v1/collector/ws" {
		t.Fatalf("unexpected url: %s", u)
	}
	if hdr.Get("Authorization") != "Bearer tok" {
		t.Fatalf("missing auth header")
	}
}
