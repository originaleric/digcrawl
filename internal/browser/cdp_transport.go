package browser

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/cdp"
)

// Send has no context in Rod. Bound both admission and the synchronous write
// without spawning a goroutine per call or changing deadlines concurrently.
type boundedCDPTransport struct {
	ws     cdp.WebSocketable
	conn   net.Conn
	close  func()
	sender chan struct{}
	limit  time.Duration
}

type cdpSendError struct {
	operation string
	cause     error
}

func (e *cdpSendError) Error() string {
	return fmt.Sprintf("%s timeout or failure: %v", e.operation, e.cause)
}
func (e *cdpSendError) Unwrap() error { return e.cause }

func (t *boundedCDPTransport) Send(data []byte) error {
	timer := time.NewTimer(t.limit)
	defer timer.Stop()
	select {
	case t.sender <- struct{}{}:
	case <-timer.C:
		return &cdpSendError{operation: "send_queue", cause: errors.New("sender busy")}
	}
	defer func() { <-t.sender }()
	if err := t.conn.SetWriteDeadline(time.Now().Add(t.limit)); err != nil {
		t.close()
		return &cdpSendError{operation: "send_write", cause: err}
	}
	err := t.ws.Send(data)
	clearErr := t.conn.SetWriteDeadline(time.Time{})
	if err != nil || clearErr != nil {
		t.close()
		return &cdpSendError{operation: "send_write", cause: errors.Join(err, clearErr)}
	}
	return nil
}
func (t *boundedCDPTransport) Read() ([]byte, error) { return t.ws.Read() }

func isLocalBrowserFailure(ctxErr, err error) bool {
	if ctxErr != nil {
		return true
	}
	var send *cdpSendError
	return errors.As(err, &send) && send.operation == "send_queue"
}

func isConnectionFailure(err error) bool {
	if err != nil && strings.Contains(err.Error(), "external target cleanup required") {
		return false
	}
	var send *cdpSendError
	if errors.As(err, &send) {
		return send.operation == "send_write"
	}
	switch browseCauseReason(err) {
	case "browser_connection_closed", "cdp_endpoint_unavailable":
		return true
	}
	return false
}
