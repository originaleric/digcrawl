package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

const (
	RuntimeName = "digcrawl"
)

var (
	RuntimeVersion = "0.1.0"
	Commit         = "unknown"
	BuildDate      = "unknown"
)

// Identity is captured once for this running executable, never recomputed
// after an upgrade has replaced the file on disk.
var buildIdentity = computeBuildIdentity()

func BuildIdentity() string { return buildIdentity }
func computeBuildIdentity() string {
	if Commit != "" && Commit != "unknown" {
		return "commit:" + Commit
	}
	executable, err := os.Executable()
	if err != nil {
		return "unavailable"
	}
	f, err := os.Open(executable)
	if err != nil {
		return "unavailable"
	}
	defer f.Close()
	sum := sha256.New()
	if _, err = io.Copy(sum, f); err != nil {
		return "unavailable"
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}
