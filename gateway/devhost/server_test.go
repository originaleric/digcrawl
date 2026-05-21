package devhost

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/originaleric/diggate/gateway/protocol"
)

func TestDevHostHandshake(t *testing.T) {
	srv := NewServer("", "/diggate/v1/collector/ws")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/diggate/v1/collector/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	hello := protocol.NewCollectorHello("inst_1", "diggate", "0.1.0")
	if err := writeEnvelope(conn, hello); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	env, err := protocol.DecodeEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != protocol.TypeCollectorHelloAck || !env.OK {
		t.Fatalf("expected hello_ack, got %+v", env)
	}
}
