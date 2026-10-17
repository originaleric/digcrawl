//go:build windows

package bridge

import "testing"

func TestWindowsHasSocketArgumentRequiresExactSocket(t *testing.T) {
	socket := `C:\Users\Jane Doe\AppData\Local\digcrawl\bridge\current.sock`
	for _, command := range []string{
		`digcrawl bridge serve --socket "C:\Users\Jane Doe\AppData\Local\digcrawl\bridge\current.sock"`,
		`digcrawl bridge serve --socket C:\Users\Jane Doe\AppData\Local\digcrawl\bridge\current.sock`,
	} {
		if !windowsHasSocketArgument(command, socket) {
			t.Fatalf("expected exact socket in %q", command)
		}
	}
	if windowsHasSocketArgument(`digcrawl bridge serve --socket C:\Users\Jane Doe\AppData\Local\digcrawl\bridge\current.sock-old`, socket) {
		t.Fatal("matched a socket prefix")
	}
}
