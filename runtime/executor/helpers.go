package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/policy"
)

// DecodeInput unmarshals ToolCall input JSON into T.
func DecodeInput[T any](call *protocol.ToolCall) (T, error) {
	var in T
	if call == nil || len(call.Input) == 0 {
		return in, fmt.Errorf("%s: empty input", policy.CodeInvalidInput)
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return in, fmt.Errorf("%s: %w", policy.CodeInvalidInput, err)
	}
	return in, nil
}

func validateCallURL(rawURL string, call *protocol.ToolCall, configDomains []string) error {
	domains := policy.MergeDomains(&call.Policy, configDomains)
	return policy.ValidateURLDomain(rawURL, domains, nil)
}

// ValidateCookieDomain checks cookie domain against URL policy.
func ValidateCookieDomain(domain string, call *protocol.ToolCall, configDomains []string) error {
	if domain == "" {
		return nil
	}
	domains := policy.MergeDomains(&call.Policy, configDomains)
	host := strings.TrimPrefix(strings.TrimSpace(domain), ".")
	return policy.ValidateURLDomain("https://"+host, domains, nil)
}

// ValidateReadPath ensures path is under one of the allowed directory prefixes.
func ValidateReadPath(path string, allowedPrefixes []string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s: path is required", policy.CodeInvalidInput)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("%s: %w", policy.CodeInvalidInput, err)
	}
	clean := filepath.Clean(abs)
	if len(allowedPrefixes) == 0 {
		return fmt.Errorf("%s: file read is disabled without Gateway.AllowedReadPaths", policy.CodeToolNotAllowed)
	}
	for _, prefix := range allowedPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		base, err := filepath.Abs(prefix)
		if err != nil {
			continue
		}
		base = filepath.Clean(base)
		if clean == base || strings.HasPrefix(clean, base+string(os.PathSeparator)) {
			return nil
		}
	}
	return fmt.Errorf("%s: path %q is not under allowed prefixes", policy.CodeToolNotAllowed, path)
}
