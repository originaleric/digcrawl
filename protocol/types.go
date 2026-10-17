package protocol

import "encoding/json"

const (
	TypeToolManifest = "tool_manifest"
	TypeToolCall     = "tool_call"
	TypeToolResult   = "tool_result"
	TypeGetManifest  = "get_manifest"
)

// ToolManifest 工具清单，供宿主发现 DigCrawl 可用能力。
type ToolManifest struct {
	Type           string           `json:"type"`
	Runtime        string           `json:"runtime"`
	BuildID        string           `json:"build_id,omitempty"`
	RuntimeVersion string           `json:"runtime_version"`
	InstanceID     string           `json:"instance_id"`
	Tools          []ToolDescriptor `json:"tools"`
}

// ToolDescriptor 单个工具的元信息。
type ToolDescriptor struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	InputSchema          json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema         json.RawMessage `json:"output_schema,omitempty"`
	Capabilities         []string        `json:"capabilities,omitempty"`
	Risk                 string          `json:"risk,omitempty"`
	RequiresUserApproval bool            `json:"requires_user_approval,omitempty"`
}

// ToolCall 宿主发起的工具调用。
type ToolCall struct {
	Type     string          `json:"type"`
	ID       string          `json:"id"`
	Tool     string          `json:"tool"`
	Input    json.RawMessage `json:"input"`
	Context  CallContext     `json:"context,omitempty"`
	Policy   CallPolicy      `json:"policy,omitempty"`
	Approved bool            `json:"approved,omitempty"`
}

// CallContext 调用上下文（审计与多租户隔离）。
type CallContext struct {
	UserID   string `json:"user_id,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
	TraceID  string `json:"trace_id,omitempty"`
	Host     string `json:"host,omitempty"`
}

// CallPolicy 单次调用的策略约束。
type CallPolicy struct {
	TimeoutMs      int      `json:"timeout_ms,omitempty"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	StoreCookies   string   `json:"store_cookies,omitempty"`
	MaxOutputBytes int      `json:"max_output_bytes,omitempty"`
	RateLimitKey   string   `json:"rate_limit_key,omitempty"`
}

// ToolResult 工具执行结果。
type ToolResult struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Status    string          `json:"status"` // success | error
	Output    json.RawMessage `json:"output,omitempty"`
	Artifacts []Artifact      `json:"artifacts,omitempty"`
	Error     *ToolError      `json:"error,omitempty"`
	Usage     Usage           `json:"usage"`
}

// ToolError 结构化错误。
type ToolError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Usage 执行用量。
type Usage struct {
	DurationMs int64 `json:"duration_ms"`
}

// Artifact 大对象引用（截图、文件等）。
type Artifact struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	URI       string `json:"uri"`
	ExpiresAt string `json:"expires_at,omitempty"`
}
