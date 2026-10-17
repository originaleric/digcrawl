package bridge

import (
	"os"
	"strings"
)

// startupStatusFile is an exit-only diagnostic handoff. It intentionally is
// not a pipe: the persistent Bridge must never inherit the stdio proxy's I/O
// lifecycle. The creator removes this private file after startup succeeds or
// reports its bounded failure text.
type startupStatusFile struct{ path string }

func (s startupStatusFile) String() string {
	if strings.TrimSpace(s.path) == "" {
		return ""
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
