package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-rod/rod"
)

// AXNode 表示无障碍树节点
type AXNode struct {
	Ref      string `json:"ref"`                // 元素引用ID，如 e0, e1, e2
	Role     string `json:"role"`               // 角色，如 button, link, textbox
	Name     string `json:"name"`               // 名称/标签
	Value    string `json:"value,omitempty"`    // 值（如输入框的值）
	Depth    int    `json:"depth"`              // 深度
	Disabled bool   `json:"disabled,omitempty"` // 是否禁用
	Focused  bool   `json:"focused,omitempty"`  // 是否聚焦
	NodeID   int64  `json:"nodeId,omitempty"`   // 后端DOM节点ID
}

// RawAXNode 表示原始无障碍树节点（来自CDP）
type RawAXNode struct {
	NodeID           string      `json:"nodeId"`
	Ignored          bool        `json:"ignored"`
	Role             *RawAXValue `json:"role"`
	Name             *RawAXValue `json:"name"`
	Value            *RawAXValue `json:"value"`
	Properties       []RawAXProp `json:"properties"`
	ChildIDs         []string    `json:"childIds"`
	BackendDOMNodeID int64       `json:"backendDOMNodeId"`
}

// RawAXValue 表示原始无障碍值
type RawAXValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// RawAXProp 表示原始无障碍属性
type RawAXProp struct {
	Name  string      `json:"name"`
	Value *RawAXValue `json:"value"`
}

// String 将RawAXValue转换为字符串
func (v *RawAXValue) String() string {
	if v == nil || v.Value == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal(v.Value, &s); err == nil {
		return s
	}
	return strings.Trim(string(v.Value), `"`)
}

// InteractiveRoles 定义可交互的角色类型
var InteractiveRoles = map[string]bool{
	"button": true, "link": true, "textbox": true, "searchbox": true,
	"combobox": true, "listbox": true, "option": true, "checkbox": true,
	"radio": true, "switch": true, "slider": true, "spinbutton": true,
	"menuitem": true, "menuitemcheckbox": true, "menuitemradio": true,
	"tab": true, "treeitem": true,
}

const FilterInteractive = "interactive"
const FilterVisible = "visible"
const FilterAll = "all"

// FetchAXTree 获取页面的无障碍树
// 返回节点列表和引用到NodeID的映射
func FetchAXTree(ctx context.Context, page *rod.Page) ([]AXNode, map[string]int64, error) {
	// 启用无障碍域
	_, err := page.Call(ctx, string(page.SessionID), "Accessibility.enable", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("启用无障碍域失败: %w", err)
	}

	// 获取完整的无障碍树
	var result struct {
		Nodes json.RawMessage `json:"nodes"`
	}
	res, err := page.Call(ctx, string(page.SessionID), "Accessibility.getFullAXTree", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("获取无障碍树失败: %w", err)
	}
	if len(res) > 0 {
		if err := json.Unmarshal(res, &result); err != nil {
			return nil, nil, fmt.Errorf("解析结果失败: %w", err)
		}
	} else {
		return nil, nil, fmt.Errorf("获取无障碍树返回空结果")
	}

	// 解析原始节点
	var rawNodes []RawAXNode
	if len(result.Nodes) > 0 {
		// result.Nodes 是 json.RawMessage 类型
		if err := json.Unmarshal(result.Nodes, &rawNodes); err != nil {
			return nil, nil, fmt.Errorf("解析无障碍树节点失败: %w", err)
		}
	}

	// 构建快照
	nodes, refs := BuildSnapshot(rawNodes, FilterAll, -1)
	return nodes, refs, nil
}

// BuildSnapshot 构建无障碍树快照
// filter: "interactive" | "visible" | "all"
// maxDepth: 最大深度，-1表示不限制
func BuildSnapshot(rawNodes []RawAXNode, filter string, maxDepth int) ([]AXNode, map[string]int64) {
	// 构建父子关系映射
	parentMap := make(map[string]string)
	for _, n := range rawNodes {
		for _, childID := range n.ChildIDs {
			parentMap[childID] = n.NodeID
		}
	}

	// 计算节点深度
	depthOf := func(nodeID string) int {
		d := 0
		cur := nodeID
		for {
			p, ok := parentMap[cur]
			if !ok {
				break
			}
			d++
			cur = p
		}
		return d
	}

	flat := make([]AXNode, 0)
	refs := make(map[string]int64)
	refID := 0

	for _, n := range rawNodes {
		if n.Ignored {
			continue
		}

		role := n.Role.String()
		name := n.Name.String()

		// 过滤掉无意义的节点
		if role == "none" || role == "generic" || role == "InlineTextBox" {
			continue
		}
		if name == "" && role == "StaticText" {
			continue
		}

		depth := depthOf(n.NodeID)
		if maxDepth >= 0 && depth > maxDepth {
			continue
		}

		// 应用过滤器
		if filter == FilterInteractive && !InteractiveRoles[role] {
			continue
		}

		// 生成引用ID
		ref := fmt.Sprintf("e%d", refID)
		entry := AXNode{
			Ref:   ref,
			Role:  role,
			Name:  name,
			Depth: depth,
		}

		// 设置值
		if v := n.Value.String(); v != "" {
			entry.Value = v
		}

		// 设置NodeID和引用映射
		if n.BackendDOMNodeID != 0 {
			entry.NodeID = n.BackendDOMNodeID
			refs[ref] = n.BackendDOMNodeID
		}

		// 解析属性
		for _, prop := range n.Properties {
			if prop.Name == "disabled" && prop.Value.String() == "true" {
				entry.Disabled = true
			}
			if prop.Name == "focused" && prop.Value.String() == "true" {
				entry.Focused = true
			}
		}

		flat = append(flat, entry)
		refID++
	}

	return flat, refs
}

// GetNodeIDByRef 通过引用ID获取NodeID
// 返回NodeID，可用于后续的CDP操作
func GetNodeIDByRef(ref string, refs map[string]int64) (int64, error) {
	nodeID, ok := refs[ref]
	if !ok {
		return 0, fmt.Errorf("引用 %s 不存在", ref)
	}
	return nodeID, nil
}

func filterElements(elements []AXNode, filter string) []AXNode {
	if filter == FilterAll {
		return elements
	}
	filtered := make([]AXNode, 0, len(elements))
	for _, elem := range elements {
		if filter == FilterInteractive && InteractiveRoles[elem.Role] {
			filtered = append(filtered, elem)
		} else if filter == FilterVisible {
			filtered = append(filtered, elem)
		}
	}
	return filtered
}
