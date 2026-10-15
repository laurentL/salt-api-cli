package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laurentL/salt-api-cli/internal/config"
)

func TestMCPServerAllTools(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"return": [{"token": "mcp-tok", "start": 1700000000.0, "expire": 1999999999.0, "user": "admin", "eauth": "pam", "perms": [".*"]}]}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var ls map[string]any
		_ = json.NewDecoder(r.Body).Decode(&ls)
		w.Header().Set("Content-Type", "application/json")
		fun, _ := ls["fun"].(string)
		switch fun {
		case "test.ping":
			_, _ = w.Write([]byte(`{"return": [{"minion-1": true}]}`))
		case "state.apply":
			_, _ = w.Write([]byte(`{"return": [{"minion-1": {"pkg_|-nginx_|-nginx_|-installed": {"result": true, "name": "nginx", "changes": {}, "comment": "ok"}}}]}`))
		case "key.list_all", "key.accept", "key.reject", "key.delete", "key.finger":
			_, _ = w.Write([]byte(`{"return": [{"tag": "salt/wheel/key", "data": {"return": {"minions": ["minion-1"]}}}]}`))
		default:
			_, _ = w.Write([]byte(`{"return": [{"minion-1": "ok"}]}`))
		}
	})
	mux.HandleFunc("/minions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"return": [{"minion-1": {"os": "Debian", "ipv4": ["10.0.0.1"]}}]}`))
	})
	mux.HandleFunc("/minions/minion-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"return": [{"minion-1": {"os": "Debian", "kernel": "Linux"}}]}`))
	})
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"return": [{"20261015001": {"Function": "test.ping", "Target": "*"}}]}`))
	})
	mux.HandleFunc("/jobs/20261015001", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"info": [{"jid": "20261015001", "Minions": ["minion-1"]}], "return": [{"minion-1": true}]}`))
	})
	mux.HandleFunc("/hook/deploy/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	cfg := &config.Config{
		DefaultProfile: "default",
		DefaultOutput:  "pretty",
		Profiles: map[string]config.Profile{
			"default": {
				URL:      srv.URL,
				Username: "admin",
				Password: "secret",
				EAuth:    "pam",
			},
		},
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save config failed: %v", err)
	}

	ov := config.Overrides{
		ConfigPath: cfgPath,
		Profile:    "default",
		URL:        srv.URL,
		Username:   "admin",
		Password:   "secret",
		Token:      "mcp-tok",
	}

	mcpSrv := NewServer(ov, "1.0.0-test", log.New(io.Discard, "", 0))
	ctx := context.Background()

	// Initialize MCP session
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	initResp := mcpSrv.HandleMessage(ctx, []byte(initReq))
	if initResp == nil {
		t.Fatalf("expected non-nil initialize response")
	}

	// Test calls across all 12 registered tools
	toolCalls := []struct {
		name string
		args map[string]any
	}{
		{"salt_list_profiles", map[string]any{}},
		{"salt_ping", map[string]any{"tgt": "*"}},
		{"salt_run", map[string]any{"tgt": "*", "fun": "cmd.run", "args": []any{"uptime"}}},
		{"salt_state_apply", map[string]any{"tgt": "*", "state": "nginx", "test": true}},
		{"salt_list_minions", map[string]any{"status_only": false}},
		{"salt_get_minion_grains", map[string]any{"minion_id": "minion-1", "grain": "os"}},
		{"salt_list_jobs", map[string]any{"limit": 5}},
		{"salt_get_job", map[string]any{"jid": "20261015001"}},
		{"salt_runner", map[string]any{"fun": "manage.up"}},
		{"salt_wheel", map[string]any{"fun": "key.list_all"}},
		{"salt_keys_manage", map[string]any{"action": "list"}},
		{"salt_send_hook", map[string]any{"tag": "deploy/test", "payload": map[string]any{"env": "prod"}}},
	}

	for i, tc := range toolCalls {
		reqObj := map[string]any{
			"jsonrpc": "2.0",
			"id":      i + 2,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      tc.name,
				"arguments": tc.args,
			},
		}
		rawReq, _ := json.Marshal(reqObj)
		resp := mcpSrv.HandleMessage(ctx, rawReq)
		rawResp, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("tool %s: marshal response failed: %v", tc.name, err)
		}
		if strings.Contains(string(rawResp), `"isError":true`) || strings.Contains(string(rawResp), `"error":`) {
			t.Fatalf("tool %s returned error response: %s", tc.name, string(rawResp))
		}
	}
}
