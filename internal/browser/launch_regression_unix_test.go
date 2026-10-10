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
	m := &browserManager{cfg: config.LocalBrowserConfig{ChromePath: bin, Headless: &headless, UserDataDir: profile}}
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
