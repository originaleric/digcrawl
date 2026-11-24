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
var executableDigest = runningExecutableDigest()
var buildIdentity = computeBuildIdentity()

func BuildIdentity() string { return buildIdentity }
func computeBuildIdentity() string {
	if Commit != "" && Commit != "unknown" {
		return "commit:" + Commit
	}
	return executableDigest
}

func runningExecutableDigest() string {
	executable, err := os.Executable()
	if err != nil {
		return "unavailable"
	}
	digest, err := fileDigest(executable)
	if err != nil {
		return "unavailable"
	}
	return digest
}

// ExecutableMatches checks the published program independently of release
// commit labels. A running watcher yields before reading another version's state.
func ExecutableMatches(path string) (bool, error) {
	digest, err := fileDigest(path)
	if err != nil {
		return false, err
	}
	return executableDigest != "unavailable" && digest == executableDigest, nil
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err = io.Copy(sum, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}
