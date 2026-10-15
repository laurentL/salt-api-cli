package saltclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/laurentL/salt-api-cli/internal/auth"
	"github.com/laurentL/salt-api-cli/internal/config"
)

func TestParseArgsAndKwargs(t *testing.T) {
	raw := []string{"nginx", "echo a=b", "test=True", "count=42", "mode=0644", `pillar={"env":"prod"}`}
	args, kwargs := ParseArgsAndKwargs(raw, false)

	if len(args) != 2 {
		t.Fatalf("expected 2 positional args, got %d (%v)", len(args), args)
	}
	if args[0] != "nginx" || args[1] != "echo a=b" {
		t.Errorf("unexpected positional args: %v", args)
	}
	if kwargs["test"] != true {
		t.Errorf("expected test=true, got %v (%T)", kwargs["test"], kwargs["test"])
	}
	if kwargs["count"] != int64(42) {
		t.Errorf("expected count=42, got %v (%T)", kwargs["count"], kwargs["count"])
	}
	if kwargs["mode"] != "0644" {
		t.Errorf("expected mode=\"0644\" string, got %v (%T)", kwargs["mode"], kwargs["mode"])
	}
	pillarMap, ok := kwargs["pillar"].(map[string]any)
	if !ok || pillarMap["env"] != "prod" {
		t.Errorf("expected parsed JSON map for pillar, got %v", kwargs["pillar"])
	}
}

func TestSaltClientFullFlow(t *testing.T) {
	var loginCount int32
	var jobPollCount int32

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&loginCount, 1)
		now := float64(time.Now().Unix())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"return": []map[string]any{
				{
					"token":  fmt.Sprintf("valid-token-%d", count),
					"start":  now,
					"expire": now + 3600,
					"user":   "saltadmin",
					"eauth":  "pam",
					"perms":  []string{".*"},
				},
			},
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		tok := r.Header.Get("X-Auth-Token")
		if tok == "expired-token" || tok == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("401 Unauthorized"))
			return
		}

		var states []LowState
		if err := json.NewDecoder(r.Body).Decode(&states); err != nil || len(states) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		st := states[0]

		switch st.Client {
		case ClientLocal:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"return": []any{
					map[string]any{"minion-1": true, "minion-2": true},
				},
			})
		case ClientLocalAsync:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"return": []any{
					map[string]any{
						"jid":     "202610150001",
						"minions": []string{"minion-1", "minion-2"},
					},
				},
			})
		case ClientWheel:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"return": []any{
					map[string]any{
						"tag": "salt/wheel/20261015",
						"data": map[string]any{
							"return": map[string]any{
								"minions":     []string{"minion-1", "minion-2"},
								"minions_pre": []string{"minion-3"},
							},
						},
					},
				},
			})
		}
	})

	mux.HandleFunc("/jobs/202610150001", func(w http.ResponseWriter, r *http.Request) {
		polls := atomic.AddInt32(&jobPollCount, 1)
		retMap := map[string]any{"minion-1": "ok-1"}
		if polls >= 2 {
			retMap["minion-2"] = "ok-2"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"info": []map[string]any{
				{"Minions": []string{"minion-1", "minion-2"}},
			},
			"return": []map[string]any{retMap},
		})
	})

	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintf(w, "tag: salt/job/123/new\ndata: {\"tag\":\"salt/job/123/new\",\"data\":{\"jid\":\"123\"}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	})

	mux.HandleFunc("/hook/deploy/app", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpDir := t.TempDir()
	store, err := auth.NewTokenStore(filepath.Join(tmpDir, "tokens.json"))
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	// Pre-seed an expired/invalid token on the server side to verify 401 auto-retry
	_ = store.Put(auth.CachedToken{
		Token:   "expired-token",
		Start:   float64(time.Now().Unix() - 10),
		Expire:  float64(time.Now().Unix() + 3600),
		User:    "saltadmin",
		EAuth:   "pam",
		Profile: "test",
		URL:     srv.URL,
	})

	prof := &config.ResolvedProfile{
		Name:      "test",
		URL:       srv.URL,
		Username:  "saltadmin",
		Password:  "secret",
		EAuth:     "pam",
		VerifySSL: false,
		Timeout:   5 * time.Second,
	}

	client, err := New(prof, store, false)
	if err != nil {
		t.Fatalf("New client failed: %v", err)
	}

	ctx := context.Background()

	// 1. RunLocal should hit 401 with "expired-token", re-login automatically, and succeed!
	res, err := client.RunLocal(ctx, "*", "test.ping", nil, nil, TargetGlob, 0)
	if err != nil {
		t.Fatalf("RunLocal failed: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["minion-1"] != true {
		t.Fatalf("unexpected RunLocal result: %v", res)
	}
	if atomic.LoadInt32(&loginCount) != 1 {
		t.Errorf("expected 1 login call after 401 retry, got %d", loginCount)
	}

	// 2. RunLocalAsync + WaitJob
	asyncRes, err := client.RunLocalAsync(ctx, "*", "cmd.run", []any{"uptime"}, nil, TargetGlob, 0)
	if err != nil {
		t.Fatalf("RunLocalAsync failed: %v", err)
	}
	if asyncRes.JID != "202610150001" {
		t.Fatalf("unexpected JID: %s", asyncRes.JID)
	}

	waitRes, err := client.WaitJob(ctx, asyncRes.JID, asyncRes.Minions, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitJob failed: %v", err)
	}
	if waitRes["minion-1"] != "ok-1" || waitRes["minion-2"] != "ok-2" {
		t.Errorf("unexpected WaitJob result: %v", waitRes)
	}

	// 3. Wheel ListKeys (unwrapped)
	keysRes, err := client.ListKeys(ctx)
	if err != nil {
		t.Fatalf("ListKeys failed: %v", err)
	}
	keysMap, ok := keysRes.(map[string]any)
	if !ok || keysMap["minions_pre"] == nil {
		t.Errorf("unexpected ListKeys result: %v", keysRes)
	}

	// 4. StreamEvents
	var events []Event
	err = client.StreamEvents(ctx, "salt/job/*", 1, func(ev Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamEvents failed: %v", err)
	}
	if len(events) != 1 || events[0].Tag != "salt/job/123/new" {
		t.Errorf("unexpected events: %+v", events)
	}

	// 5. SendWebhook
	hookRes, err := client.SendWebhook(ctx, "deploy/app", map[string]any{"version": "1.0"})
	if err != nil {
		t.Fatalf("SendWebhook failed: %v", err)
	}
	if hm, ok := hookRes.(map[string]any); !ok || hm["success"] != true {
		t.Errorf("unexpected hook response: %v", hookRes)
	}
}
