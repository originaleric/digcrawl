package policy

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/originaleric/digcrawl/protocol"
)

// Error codes aligned with gateway protocol.
const (
	CodeDomainNotAllowed = "DOMAIN_NOT_ALLOWED"
	CodeToolNotAllowed   = "TOOL_NOT_ALLOWED"
	CodeInvalidInput     = "INVALID_INPUT"
)

// ValidateToolAllowed checks tool name against gateway allowlist.
func ValidateToolAllowed(toolName string, allowedTools []string) error {
	if len(allowedTools) == 0 {
		return nil
	}
	for _, t := range allowedTools {
		if strings.TrimSpace(t) == toolName {
			return nil
		}
	}
	return fmt.Errorf("%s: tool %q is not allowed", CodeToolNotAllowed, toolName)
}

// ValidateURLDomain validates http(s) URL and optional domain allowlists.
// callDomains from ToolCall.Policy take precedence when non-empty; otherwise configDomains apply.
func ValidateURLDomain(rawURL string, callDomains, configDomains []string) error {
	u, err := parseHTTPURL(rawURL)
	if err != nil {
		return err
	}
	allowed := callDomains
	if len(allowed) == 0 {
		allowed = configDomains
	}
	return checkAllowedDomain(u.Hostname(), allowed)
}

func parseHTTPURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("%s: invalid url: %w", CodeInvalidInput, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%s: only http/https URLs are supported", CodeInvalidInput)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("%s: url missing host", CodeInvalidInput)
	}
	return u, nil
}

func checkAllowedDomain(host string, allowedDomains []string) error {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return fmt.Errorf("%s: empty domain", CodeInvalidInput)
	}
	if len(allowedDomains) == 0 {
		return nil
	}
	for _, domain := range allowedDomains {
		d := strings.ToLower(strings.TrimSpace(domain))
		if d == "" {
			continue
		}
		if h == d || strings.HasSuffix(h, "."+d) {
			return nil
		}
	}
	return fmt.Errorf("%s: target domain %q is not allowed", CodeDomainNotAllowed, host)
}

// MergeDomains returns call-level domains when set, otherwise config domains.
func MergeDomains(call *protocol.CallPolicy, configDomains []string) []string {
	if call != nil && len(call.AllowedDomains) > 0 {
		return call.AllowedDomains
	}
	return configDomains
}
