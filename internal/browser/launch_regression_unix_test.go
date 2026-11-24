//go:build !windows

package browser

import (
	"context"
	"errors"
	"github.com/originaleric/digcrawl/config"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLaunchDiscoveryStallTerminatesItsOwnerAndReleasesProfile(t *testing.T) {
	for _, bodyStall := range []bool{false, true} {
		t.Run(map[bool]string{false: "headers", true: "body"}[bodyStall], func(t *testing.T) { testLaunchDiscoveryStall(t, bodyStall) })
	}
}
func testLaunchDiscoveryStall(t *testing.T, bodyStall bool) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 fault fixture unavailable")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "chrome-fixture")
	script := "#!" + python + "\n" + `import http.server, sys, time
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  if ` + map[bool]string{false: "False", true: "True"}[bodyStall] + `:
   self.send_response(200)
   self.send_header('Content-Length','1000')
   self.end_headers()
   self.wfile.write(b'{')
   self.wfile.flush()
  time.sleep(60)
 def log_message(self,*args): pass
server = http.server.HTTPServer(('127.0.0.1',0),Handler)
print('DevTools listening on ws://127.0.0.1:%d/devtools/browser/fixture' % server.server_port,file=sys.stderr,flush=True)
server.serve_forever()
`
	if err = os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	headless := true
	profile := filepath.Join(dir, "profile")
	m := &browserManager{resolveRuntimeFn: func() error { return nil }, cfg: config.LocalBrowserConfig{ChromePath: bin, Headless: &headless, UserDataDir: profile}}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	c, err := m.openBrowserConnection(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || c == nil || c.launcher.PID() <= 0 {
		t.Fatalf("partial launch conn=%v err=%v", c, err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("discovery ignored setup budget")
	}
	cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err = c.closeResources(cleanup); err != nil {
		t.Fatal(err)
	}
	lock, _, err := AcquireProfileLock(profile)
	if err != nil {
		t.Fatal("partial startup retained profile guard:", err)
	}
	_ = lock.Close()
}

func TestOwnedProcessRecordedBeforeCDPConnectionFailure(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "CDP_failure", true: "record_failure"}[reject], func(t *testing.T) { testOwnedProcessRecord(t, reject) })
	}
}

func testOwnedProcessRecord(t *testing.T, reject bool) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "chrome-fixture")
	script := "#!" + python + "\n" + `import http.server,json,sys
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  data=json.dumps({'webSocketDebuggerUrl':'ws://127.0.0.1:%d/devtools/browser/test'%self.server.server_port}).encode()
  self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
 def log_message(self,*args):pass
server=http.server.HTTPServer(('127.0.0.1',0),Handler)
print('DevTools listening on ws://127.0.0.1:%d/devtools/browser/test'%server.server_port,file=sys.stderr,flush=True)
server.serve_forever()
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	recorded := 0
	var recordErr error
	if reject {
		recordErr = errors.New("test ownership publication rejected")
	}
	finish, err := RegisterOwnedProcessRecorder(func(pid int) error { recorded = pid; return recordErr })
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	headless := true
	manager := &browserManager{resolveRuntimeFn: func() error { return nil }, cfg: config.LocalBrowserConfig{ChromePath: bin, Headless: &headless, UserDataDir: filepath.Join(dir, "profile")}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := manager.openBrowserConnection(ctx)
	if err == nil || conn == nil || recorded <= 0 || recorded != conn.launcher.PID() {
		t.Fatalf("ownership missing before CDP failure: recorded=%d conn=%v err=%v", recorded, conn, err)
	}
	if recordErr != nil && !errors.Is(err, recordErr) {
		t.Fatalf("record failure was lost: %v", err)
	}
	cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := conn.closeResources(cleanup); err != nil {
		t.Fatal(err)
	}
}
