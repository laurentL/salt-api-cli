package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
)

// ToolHandlerFunc handles an incoming MCP tools/call request.
type ToolHandlerFunc func(ctx context.Context, req CallToolRequest) (*CallToolResult, error)

// Tool represents an MCP tool definition and its JSON Schema input specification.
type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	InputSchema ToolSchema `json:"inputSchema"`
}

// ToolSchema describes the JSON Schema object for tool arguments.
type ToolSchema struct {
	Type       string                   `json:"type"`
	Properties map[string]PropertyEntry `json:"properties,omitempty"`
	Required   []string                 `json:"required,omitempty"`
}

// PropertyEntry describes a single property within a ToolSchema.
type PropertyEntry map[string]any

// ToolOption configures a Tool.
type ToolOption func(*Tool)

// PropertyOption configures a PropertyEntry inside a ToolSchema.
type PropertyOption func(name string, t *Tool, prop PropertyEntry)

// NewTool constructs a Tool with the given name and options.
func NewTool(name string, opts ...ToolOption) Tool {
	t := Tool{
		Name: name,
		InputSchema: ToolSchema{
			Type:       "object",
			Properties: make(map[string]PropertyEntry),
		},
	}
	for _, opt := range opts {
		opt(&t)
	}
	return t
}

func WithDescription(desc string) ToolOption {
	return func(t *Tool) {
		t.Description = desc
	}
}

func withProperty(propType string, name string, opts ...PropertyOption) ToolOption {
	return func(t *Tool) {
		prop := PropertyEntry{"type": propType}
		for _, opt := range opts {
			opt(name, t, prop)
		}
		t.InputSchema.Properties[name] = prop
	}
}

func WithString(name string, opts ...PropertyOption) ToolOption {
	return withProperty("string", name, opts...)
}

func WithBoolean(name string, opts ...PropertyOption) ToolOption {
	return withProperty("boolean", name, opts...)
}

func WithNumber(name string, opts ...PropertyOption) ToolOption {
	return withProperty("number", name, opts...)
}

func WithArray(name string, opts ...PropertyOption) ToolOption {
	return withProperty("array", name, opts...)
}

func WithObject(name string, opts ...PropertyOption) ToolOption {
	return withProperty("object", name, opts...)
}

func Required() PropertyOption {
	return func(name string, t *Tool, _ PropertyEntry) {
		t.InputSchema.Required = append(t.InputSchema.Required, name)
	}
}

func Description(desc string) PropertyOption {
	return func(_ string, _ *Tool, prop PropertyEntry) {
		prop["description"] = desc
	}
}

func DefaultString(val string) PropertyOption {
	return func(_ string, _ *Tool, prop PropertyEntry) {
		prop["default"] = val
	}
}

func WithStringItems() PropertyOption {
	return func(_ string, _ *Tool, prop PropertyEntry) {
		prop["items"] = map[string]any{"type": "string"}
	}
}

func Enum(values ...string) PropertyOption {
	return func(_ string, _ *Tool, prop PropertyEntry) {
		prop["enum"] = values
	}
}

// CallToolRequest represents the parameters of a tools/call invocation.
type CallToolRequest struct {
	Name      string
	Arguments map[string]any
}

func (r CallToolRequest) GetArguments() map[string]any {
	if r.Arguments == nil {
		return map[string]any{}
	}
	return r.Arguments
}

func (r CallToolRequest) GetString(key, def string) string {
	v, ok := r.Arguments[key]
	if !ok || v == nil {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func (r CallToolRequest) RequireString(key string) (string, error) {
	v, ok := r.Arguments[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required parameter %q", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("parameter %q must be a non-empty string", key)
	}
	return s, nil
}

func (r CallToolRequest) GetBool(key string, def bool) bool {
	v, ok := r.Arguments[key]
	if !ok || v == nil {
		return def
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		b, err := strconv.ParseBool(val)
		if err == nil {
			return b
		}
	}
	return def
}

func (r CallToolRequest) GetInt(key string, def int) int {
	v, ok := r.Arguments[key]
	if !ok || v == nil {
		return def
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	case string:
		i, err := strconv.Atoi(val)
		if err == nil {
			return i
		}
	}
	return def
}

func (r CallToolRequest) GetStringSlice(key string, def []string) []string {
	v, ok := r.Arguments[key]
	if !ok || v == nil {
		return def
	}
	switch slice := v.(type) {
	case []string:
		return slice
	case []any:
		out := make([]string, 0, len(slice))
		for _, item := range slice {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	}
	return def
}

// TextContent represents a text content block in an MCP CallToolResult.
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallToolResult is the response payload for tools/call.
type CallToolResult struct {
	Content []TextContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

func NewToolResultText(text string) *CallToolResult {
	return &CallToolResult{
		Content: []TextContent{{Type: "text", Text: text}},
	}
}

func NewToolResultError(msg string) *CallToolResult {
	return &CallToolResult{
		Content: []TextContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}

type registeredTool struct {
	tool    Tool
	handler ToolHandlerFunc
}

// MCPServer implements a lightweight, zero-dependency JSON-RPC 2.0 MCP server over stdio.
type MCPServer struct {
	name      string
	version   string
	tools     []registeredTool
	toolsByNm map[string]registeredTool
	logger    *log.Logger
}

func NewMCPServer(name, version string, logger *log.Logger) *MCPServer {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &MCPServer{
		name:      name,
		version:   version,
		toolsByNm: make(map[string]registeredTool),
		logger:    logger,
	}
}

func (s *MCPServer) AddTool(t Tool, h ToolHandlerFunc) {
	rt := registeredTool{tool: t, handler: h}
	s.tools = append(s.tools, rt)
	s.toolsByNm[t.Name] = rt
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// HandleMessage processes a single JSON-RPC 2.0 request frame and returns a response object (or nil for notifications).
func (s *MCPServer) HandleMessage(ctx context.Context, raw []byte) any {
	var req jsonRPCRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &jsonRPCError{Code: -32700, Message: "Parse error"},
		}
	}

	// JSON-RPC notifications have no ID and expect no response frame
	if len(req.ID) == 0 {
		return nil
	}

	switch req.Method {
	case "initialize":
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    s.name,
					"version": s.version,
				},
			},
		}

	case "ping":
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		}

	case "tools/list":
		list := make([]Tool, 0, len(s.tools))
		for _, rt := range s.tools {
			list = append(list, rt.tool)
		}
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": list,
			},
		}

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonRPCError{Code: -32602, Message: fmt.Sprintf("Invalid params: %v", err)},
			}
		}
		rt, exists := s.toolsByNm[params.Name]
		if !exists {
			return jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonRPCError{Code: -32601, Message: fmt.Sprintf("Unknown tool %q", params.Name)},
			}
		}
		res, err := rt.handler(ctx, CallToolRequest{
			Name:      params.Name,
			Arguments: params.Arguments,
		})
		if err != nil {
			res = NewToolResultError(err.Error())
		}
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  res,
		}

	default:
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)},
		}
	}
}

// ServeReaderWriter runs the JSON-RPC stdio loop over the given reader and writer.
func (s *MCPServer) ServeReaderWriter(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	// Support large payloads up to 16 MiB
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		resp := s.HandleMessage(ctx, line)
		if resp == nil {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			s.logger.Printf("Failed to encode response: %v", err)
			return err
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return os.Stdin.Close()
}
