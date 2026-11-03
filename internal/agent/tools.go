package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type toolCtx struct {
	ctx context.Context
	rt  *Runtime
	run *run
}

type toolResult struct {
	Text    string
	Image   []byte // JPEG screenshot to show the model
	Kind    string // browser_state | long
	Short   string // replacement when pruned from context
	Suspend *Needs
}

type tool struct {
	Name        string
	Description string
	Params      map[string]any // properties
	Required    []string
	NoStatus    bool
	Run         func(tc *toolCtx, args map[string]any) (*toolResult, error)
}

func (t *tool) schema() map[string]any {
	props := map[string]any{}
	for k, v := range t.Params {
		props[k] = v
	}
	req := append([]string{}, t.Required...)
	if !t.NoStatus {
		props["status"] = map[string]any{"type": "string", "description": "Short user-facing step label, present progressive, e.g. \"Opening the library website\"."}
		props["done"] = map[string]any{"type": "string", "description": "The same step in past tense, e.g. \"Opened the library website\"."}
		req = append(req, "status")
	}
	return map[string]any{
		"type":        "function",
		"name":        t.Name,
		"description": t.Description,
		"parameters": map[string]any{
			"type":       "object",
			"properties": props,
			"required":   req,
		},
	}
}

// buildTools lists every tool in the order the model sees them. The order and
// wording are part of the cached prompt prefix (TestToolSchemasGolden pins
// them), so each group lives in its own tools_*.go file and a new tool goes at
// the end of its group.
func (rt *Runtime) buildTools() []*tool {
	tools := browserTools()
	tools = append(tools, browserLoginTool(), requestLoginTool())
	tools = append(tools, userTools()...)
	tools = append(tools, planTool())
	tools = append(tools, webTools()...)
	tools = append(tools, fileTools()...)
	tools = append(tools, memoryTools()...)
	tools = append(tools, scheduleTools()...)
	if rt.cfg.ShellEnabled {
		tools = append(tools, shellTool())
	}
	if rt.cfg.ImageModel != "" {
		tools = append(tools, imageTool())
	}
	return tools
}

func str(a map[string]any, k string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func num(a map[string]any, k string, def float64) float64 {
	switch v := a[k].(type) {
	case float64:
		return v
	case string:
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			return f
		}
	}
	return def
}

func boolean(a map[string]any, k string, def bool) bool {
	switch v := a[k].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return def
}

func strList(a map[string]any, k string) []string {
	var out []string
	if arr, ok := a[k].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func p(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

func marshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
