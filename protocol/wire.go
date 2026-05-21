package protocol

import "encoding/json"

// WebSocket 线协议消息类型（Collector ↔ 宿主，宿主无关）。
const (
	TypeCollectorHello    = "collector_hello"
	TypeCollectorHelloAck = "collector_hello_ack"
	TypeCollectorManifest = "collector_manifest"
	TypeInstanceStatus    = "instance_status"
	TypePing              = "ping"
	TypePong              = "pong"
	TypePullTasks         = "pull_tasks"
	TypePullTasksAck      = "pull_tasks_ack"
	TypeWireError         = "error"
)

// Envelope 是 WebSocket 上的统一消息外壳。
type Envelope struct {
	Type string `json:"type"`

	// CollectorHello
	InstanceID     string `json:"instance_id,omitempty"`
	Runtime        string `json:"runtime,omitempty"`
	RuntimeVersion string `json:"runtime_version,omitempty"`

	// CollectorHelloAck
	SessionID string `json:"session_id,omitempty"`
	OK        bool   `json:"ok,omitempty"`
	Message   string `json:"message,omitempty"`

	// InstanceStatus
	Status       string `json:"status,omitempty"` // online | busy | draining
	ActiveCalls  int    `json:"active_calls,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`

	// PullTasks
	Limit int `json:"limit,omitempty"`

	// PullTasksAck
	Calls []ToolCall `json:"calls,omitempty"`

	// 内嵌标准工具协议
	Manifest  *ToolManifest `json:"manifest,omitempty"`
	ToolCall  *ToolCall     `json:"tool_call,omitempty"`
	ToolResult *ToolResult  `json:"tool_result,omitempty"`
	Error     *ToolError    `json:"error,omitempty"`
}

// CollectorHello 建连握手（Collector → 宿主）。
func NewCollectorHello(instanceID, runtime, version string) Envelope {
	return Envelope{
		Type:           TypeCollectorHello,
		InstanceID:     instanceID,
		Runtime:        runtime,
		RuntimeVersion: version,
	}
}

// NewCollectorManifest 上报工具清单。
func NewCollectorManifest(m ToolManifest) Envelope {
	return Envelope{
		Type:     TypeCollectorManifest,
		Manifest: &m,
	}
}

// NewInstanceStatus 上报实例状态（兼心跳）。
func NewInstanceStatus(instanceID, status string, activeCalls int) Envelope {
	return Envelope{
		Type:         TypeInstanceStatus,
		InstanceID:   instanceID,
		Status:       status,
		ActiveCalls:  activeCalls,
		Capabilities: []string{"push", "pull"},
	}
}

// NewPullTasks 拉取待执行任务。
func NewPullTasks(limit int) Envelope {
	return Envelope{
		Type:  TypePullTasks,
		Limit: limit,
	}
}

// NewToolResultEnvelope 回传执行结果。
func NewToolResultEnvelope(r ToolResult) Envelope {
	return Envelope{
		Type:       TypeToolResult,
		ToolResult: &r,
	}
}

// NewWireError 协议层错误。
func NewWireError(code, message string) Envelope {
	return Envelope{
		Type: TypeWireError,
		Error: &ToolError{
			Code:    code,
			Message: message,
		},
	}
}

// DecodeEnvelope parses a WebSocket text frame.
func DecodeEnvelope(data []byte) (Envelope, error) {
	var peek struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return Envelope{}, err
	}
	switch peek.Type {
	case TypeToolCall:
		var call ToolCall
		if err := json.Unmarshal(data, &call); err != nil {
			return Envelope{}, err
		}
		return Envelope{Type: TypeToolCall, ToolCall: &call}, nil
	case TypeToolResult:
		var res ToolResult
		if err := json.Unmarshal(data, &res); err != nil {
			return Envelope{}, err
		}
		return Envelope{Type: TypeToolResult, ToolResult: &res}, nil
	case TypeToolManifest:
		var m ToolManifest
		if err := json.Unmarshal(data, &m); err != nil {
			return Envelope{}, err
		}
		return Envelope{Type: TypeCollectorManifest, Manifest: &m}, nil
	default:
		var env Envelope
		err := json.Unmarshal(data, &env)
		return env, err
	}
}

// Encode serializes an envelope.
func (e Envelope) Encode() ([]byte, error) {
	return json.Marshal(e)
}
