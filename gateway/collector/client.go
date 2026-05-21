package collector

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/originaleric/diggate/gateway/gwversion"
	"github.com/originaleric/diggate/gateway/protocol"
	"github.com/originaleric/diggate/gateway/runtime"
)

// envelopeWriter serializes all outbound WebSocket text frames through one mutex.
// gorilla/websocket does not permit concurrent Conn.WriteMessage.
type envelopeWriter func(env protocol.Envelope) error

// Client maintains a reverse WebSocket connection to a host and executes tool calls.
type Client struct {
	opts    Options
	rt      *runtime.Runtime
	limiter *RateLimiter
	log     *log.Logger

	activeCalls atomic.Int32
}

func NewClient(opts Options, rt *runtime.Runtime) *Client {
	return &Client{
		opts:    opts,
		rt:      rt,
		limiter: NewRateLimiter(time.Second),
		log:     log.Default(),
	}
}

// Run connects to the host and blocks until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	if c.opts.ServerURL == "" {
		return errEmptyServerURL
	}
	for {
		if err := c.connectOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			c.log.Printf("[collector] session ended: %v; reconnect in %s", err, c.opts.ReconnectDelay)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.opts.ReconnectDelay):
		}
	}
}

func (c *Client) connectOnce(ctx context.Context) error {
	wsURL, hdr, err := buildWSURL(c.opts.ServerURL, c.opts.WSPath, c.opts.Token)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, wsURL, hdr)
	if err != nil {
		return err
	}
	defer conn.Close()

	c.log.Printf("[collector] connected to %s instance=%s", wsURL, c.opts.InstanceID)

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var writeMu sync.Mutex
	writeEnv := envelopeWriter(func(env protocol.Envelope) error {
		data, encErr := env.Encode()
		if encErr != nil {
			return encErr
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, data)
	})

	writeRaw := func(msgType int, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(msgType, payload)
	}

	if err := c.handshake(sessionCtx, conn, writeEnv); err != nil {
		return err
	}

	sem := make(chan struct{}, c.opts.MaxConcurrentCalls)
	var wg sync.WaitGroup
	defer wg.Wait()

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.readLoop(sessionCtx, conn, writeEnv, sem, &wg)
	}()

	if c.opts.HeartbeatInterval > 0 {
		go c.heartbeatLoop(sessionCtx, writeEnv)
	}
	if c.opts.PullInterval > 0 {
		go c.pullLoop(sessionCtx, writeEnv)
	}

	select {
	case <-ctx.Done():
		cancel()
		_ = writeRaw(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		return ctx.Err()
	case err := <-errCh:
		cancel()
		return err
	}
}

func (c *Client) handshake(_ context.Context, conn *websocket.Conn, writeEnv envelopeWriter) error {
	hello := protocol.NewCollectorHello(c.opts.InstanceID, gwversion.RuntimeName, gwversion.RuntimeVersion)
	if err := writeEnv(hello); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})
	env, err := protocol.DecodeEnvelope(data)
	if err != nil {
		return err
	}
	if env.Type == protocol.TypeWireError {
		return errHelloRejected
	}
	if env.Type != protocol.TypeCollectorHelloAck || !env.OK {
		msg := env.Message
		if msg == "" {
			msg = "hello not acknowledged"
		}
		c.log.Printf("[collector] hello rejected: %s", msg)
		return errHelloRejected
	}

	manifest := c.rt.Manifest()
	if err := writeEnv(protocol.NewCollectorManifest(manifest)); err != nil {
		return err
	}
	status := protocol.NewInstanceStatus(c.opts.InstanceID, "online", 0)
	return writeEnv(status)
}

func (c *Client) heartbeatLoop(ctx context.Context, writeEnv envelopeWriter) {
	ticker := time.NewTicker(c.opts.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			busy := "online"
			active := int(c.activeCalls.Load())
			if active > 0 {
				busy = "busy"
			}
			env := protocol.NewInstanceStatus(c.opts.InstanceID, busy, active)
			if err := writeEnv(env); err != nil {
				return
			}
		}
	}
}

func (c *Client) pullLoop(ctx context.Context, writeEnv envelopeWriter) {
	ticker := time.NewTicker(c.opts.PullInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.activeCalls.Load() >= int32(c.opts.MaxConcurrentCalls) {
				continue
			}
			if err := writeEnv(protocol.NewPullTasks(c.opts.PullBatchSize)); err != nil {
				return
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, writeEnv envelopeWriter, sem chan struct{}, wg *sync.WaitGroup) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		env, err := protocol.DecodeEnvelope(data)
		if err != nil {
			c.log.Printf("[collector] invalid envelope: %v", err)
			continue
		}
		if err := c.dispatch(ctx, writeEnv, env, sem, wg); err != nil {
			return err
		}
	}
}

func (c *Client) dispatch(ctx context.Context, writeEnv envelopeWriter, env protocol.Envelope, sem chan struct{}, wg *sync.WaitGroup) error {
	switch env.Type {
	case protocol.TypePing:
		return writeEnv(protocol.Envelope{Type: protocol.TypePong})
	case protocol.TypePong, protocol.TypeCollectorHelloAck:
		return nil
	case protocol.TypeToolCall:
		if env.ToolCall == nil {
			return nil
		}
		c.scheduleCall(ctx, writeEnv, *env.ToolCall, sem, wg)
		return nil
	case protocol.TypePullTasksAck:
		for i := range env.Calls {
			c.scheduleCall(ctx, writeEnv, env.Calls[i], sem, wg)
		}
		return nil
	case protocol.TypeWireError:
		if env.Error != nil {
			c.log.Printf("[collector] host error: %s %s", env.Error.Code, env.Error.Message)
		}
		return nil
	default:
		return nil
	}
}

func (c *Client) scheduleCall(ctx context.Context, writeEnv envelopeWriter, call protocol.ToolCall, sem chan struct{}, wg *sync.WaitGroup) {
	select {
	case sem <- struct{}{}:
	default:
		c.log.Printf("[collector] dropping call %s: max concurrent reached", call.ID)
		res := protocol.ToolResult{
			Type:   protocol.TypeToolResult,
			ID:     call.ID,
			Status: "error",
			Error: &protocol.ToolError{
				Code:    "RATE_LIMITED",
				Message: "collector at max concurrent calls",
			},
		}
		_ = writeEnv(protocol.NewToolResultEnvelope(res))
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { <-sem }()
		c.executeAndReply(ctx, writeEnv, call)
	}()
}

func (c *Client) executeAndReply(ctx context.Context, writeEnv envelopeWriter, call protocol.ToolCall) {
	if call.Type == "" {
		call.Type = protocol.TypeToolCall
	}
	key := call.Policy.RateLimitKey
	if !c.limiter.Check(key) {
		res := protocol.ToolResult{
			Type:   protocol.TypeToolResult,
			ID:     call.ID,
			Status: "error",
			Error: &protocol.ToolError{
				Code:    "RATE_LIMITED",
				Message: "rate limit cooldown active",
			},
		}
		_ = writeEnv(protocol.NewToolResultEnvelope(res))
		return
	}

	c.activeCalls.Add(1)
	defer c.activeCalls.Add(-1)
	defer c.limiter.Touch(key)

	result := c.rt.Execute(ctx, &call)
	if err := writeEnv(protocol.NewToolResultEnvelope(*result)); err != nil {
		c.log.Printf("[collector] failed to send result for %s: %v", call.ID, err)
	}
}

func writeEnvelope(conn *websocket.Conn, env protocol.Envelope) error {
	data, err := env.Encode()
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}
