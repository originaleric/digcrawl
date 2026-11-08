package bridge

import (
	"debug/buildinfo"
	"reflect"
	"strings"
)

// OwnerDiagnostic belongs to the existing Bridge ownership boundary. It is a
// read-only observation, never a second registry or an authorization to kill.
type OwnerDiagnostic struct {
	PID        int
	Executable string
	Verified   bool
	identity   string
}

// A renamed CLI is still DigCrawl; a shell that merely mentions a worker is
// not. Inspect Go's embedded package identity rather than trusting a basename.
func isBridgeExecutable(path string) bool {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return false
	}
	module := strings.TrimSuffix(reflect.TypeOf(OwnerDiagnostic{}).PkgPath(), "/transport/bridge")
	return info.Path == module+"/cmd/digcrawl" || (strings.HasPrefix(info.Path, module+"/") && strings.HasSuffix(info.Path, ".test"))
}
