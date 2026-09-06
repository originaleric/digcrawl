package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const OCRImageAccept = "image/jpeg,image/png,image/webp,image/*;q=0.8,*/*;q=0.5"

// NewSafeHTTPClient returns the HTTP client used for remote media acquisition.
// It validates redirect targets and resolves each connection target before
// dialing so a hostname cannot redirect or re-resolve to a local address.
// Proxying is disabled because it would hide the final network target from
// this local downloader.
func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = safeDialContext
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return ValidateRemoteURL(req.URL.String())
		},
	}
}

// ValidateRemoteURL rejects malformed, local, and credential-bearing remote
// media URLs before a caller creates a request. NewSafeHTTPClient performs the
// complementary DNS and dial-time checks for named hosts.
func ValidateRemoteURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("unsupported remote media scheme %q", u.Scheme)
	}
	if u.User != nil {
		return errors.New("remote media URL userinfo is not supported")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return errors.New("remote media URL missing host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrLocalIP(ip) {
			return fmt.Errorf("remote media host %q is private/local", host)
		}
		return nil
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("remote media host %q is local", host)
	}
	return nil
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse remote media address %q: %w", address, err)
	}
	ips, err := resolvePublicHost(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{}
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("dial remote media host %q: %w", host, lastErr)
}

func resolvePublicHost(ctx context.Context, host string) ([]net.IP, error) {
	return resolvePublicHostWithLookup(ctx, host, net.DefaultResolver.LookupIPAddr)
}

func resolvePublicHostWithLookup(ctx context.Context, host string, lookup func(context.Context, string) ([]net.IPAddr, error)) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrLocalIP(ip) {
			return nil, fmt.Errorf("remote media host %q is private/local", host)
		}
		return []net.IP{ip}, nil
	}
	addresses, err := lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve remote media host %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve remote media host %q: no addresses", host)
	}
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if isPrivateOrLocalIP(address.IP) {
			return nil, fmt.Errorf("remote media host %q resolved to private/local address %q", host, address.IP)
		}
		ips = append(ips, address.IP)
	}
	return ips, nil
}

func isPrivateOrLocalIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// Carrier-grade NAT and the reserved 240.0.0.0/4 range are not
		// publicly routable destinations even though net.IP.IsPrivate does
		// not classify them as RFC1918 private addresses.
		return v4[0] == 100 && v4[1]&0xc0 == 0x40 || v4[0] >= 240
	}
	return false
}

// PreferOCRCompatibleImageFormat asks an image CDN for formats accepted by
// the configured OpenAI-compatible OCR providers. It intentionally leaves
// audio/video requests untouched.
func PreferOCRCompatibleImageFormat(req *http.Request, mediaType string) {
	if req == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image", "photo", "screenshot":
		req.Header.Set("Accept", OCRImageAccept)
	}
}

// DownloadFile is the shared bounded media download path for Skill and
// Collect. It owns HTTP negotiation that affects OCR compatibility while the
// callers retain their distinct artifact and export delivery policies.
func DownloadFile(ctx context.Context, client *http.Client, rawURL, dest string, maxBytes int64, referer, mediaType string) (string, error) {
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(dest)
		}
	}()
	if client == nil {
		client = NewSafeHTTPClient(0)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	PreferOCRCompatibleImageFormat(req, mediaType)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}
	if strings.EqualFold(strings.TrimSpace(mediaType), "video") && clearlyNonVideoContentType(resp.Header.Get("Content-Type")) {
		return "", fmt.Errorf("response is not a video")
	}
	if resp.ContentLength > maxBytes {
		return "", fmt.Errorf("content length %d exceeds max %d", resp.ContentLength, maxBytes)
	}
	target, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	written, copyErr := io.Copy(target, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := target.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > maxBytes {
		return "", fmt.Errorf("download exceeds max bytes %d", maxBytes)
	}
	if strings.EqualFold(strings.TrimSpace(mediaType), "video") {
		if err := ValidateVideoFile(dest); err != nil {
			return "", err
		}
	}
	completed = true
	return resp.Header.Get("Content-Type"), nil
}

func clearlyNonVideoContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType == "" || contentType == "application/octet-stream" {
		return false
	}
	return !strings.HasPrefix(contentType, "video/")
}

// ValidateVideoFile verifies the small, format-level invariant shared by all
// durable video paths. It intentionally accepts only containers that the
// local media pipeline already supports, rather than trusting a URL suffix or
// HTTP content type supplied by a remote service.
func ValidateVideoFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 12)
	n, err := io.ReadFull(f, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return err
	}
	if n >= 8 && string(header[4:8]) == "ftyp" {
		return nil
	}
	if n >= 4 && string(header[:4]) == "\x1aE\xdf\xa3" {
		return nil
	}
	return errors.New("downloaded response is not a recognized video file")
}
