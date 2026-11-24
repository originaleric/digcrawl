package browser

import (
	"fmt"
	"sync"
)

// The managed Bridge persists ownership before this browser becomes usable.
// Serialize registration and removal so a late launch cannot write after its
// Bridge has released the lifecycle lock.
var ownedProcessRecord struct {
	sync.Mutex
	generation uint64
	write      func(int) error
}

func RegisterOwnedProcessRecorder(write func(int) error) (func(), error) {
	if write == nil {
		return nil, fmt.Errorf("browser process recorder is required")
	}
	ownedProcessRecord.Lock()
	defer ownedProcessRecord.Unlock()
	if ownedProcessRecord.write != nil {
		return nil, fmt.Errorf("browser process recorder already belongs to a Bridge")
	}
	ownedProcessRecord.generation++
	ownedProcessRecord.write = write
	generation := ownedProcessRecord.generation
	return func() {
		ownedProcessRecord.Lock()
		defer ownedProcessRecord.Unlock()
		if generation != ownedProcessRecord.generation {
			return
		}
		ownedProcessRecord.generation++
		ownedProcessRecord.write = nil
	}, nil
}

// Capture before launch. An opener from a retired Bridge cannot register its
// eventual process against a later Bridge's record.
func ownedProcessRecorder() func(int) error {
	ownedProcessRecord.Lock()
	generation := ownedProcessRecord.generation
	write := ownedProcessRecord.write
	ownedProcessRecord.Unlock()
	return func(pid int) error {
		if write == nil || pid <= 0 {
			return nil
		}
		ownedProcessRecord.Lock()
		defer ownedProcessRecord.Unlock()
		if generation != ownedProcessRecord.generation {
			return fmt.Errorf("browser launch outlived its Bridge owner")
		}
		return write(pid)
	}
}
