package browser

import "testing"

func TestLateBrowserLaunchCannotWriteNewBridgeRecord(t *testing.T) {
	writes := 0
	first, err := RegisterOwnedProcessRecorder(func(int) error { writes++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	oldLaunch := ownedProcessRecorder()
	first()
	second, err := RegisterOwnedProcessRecorder(func(int) error { writes++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	first() // late duplicate cleanup belongs only to the original recorder
	if err := oldLaunch(123); err == nil || writes != 0 {
		t.Fatalf("late launch wrote ownership: %v %d", err, writes)
	}
	if err := ownedProcessRecorder()(123); err != nil || writes != 1 {
		t.Fatalf("current recorder failed: %v %d", err, writes)
	}
}
