package collector

import (
	"net/http"
	"net/url"
	"strings"
)

func buildWSURL(serverURL, wsPath, token string) (string, http.Header, error) {
	raw := strings.TrimSpace(serverURL)
	if raw == "" {
		return "", nil, errEmptyServerURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", nil, err
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
		// already ws scheme
	default:
		return "", nil, errInvalidServerScheme
	}
	path := strings.TrimSpace(wsPath)
	if path == "" {
		path = "/digcrawl/v1/collector/ws"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	u.RawQuery = ""
	u.Fragment = ""

	hdr := make(http.Header)
	if token != "" {
		hdr.Set("Authorization", "Bearer "+token)
	}
	return u.String(), hdr, nil
}
