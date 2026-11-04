package saltclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/laurentL/salt-api-cli/internal/auth"
	"github.com/laurentL/salt-api-cli/internal/config"
)

// newWaitTestClient builds a client against srv with a pre-seeded valid token.
func newWaitTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	store, err := auth.NewTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	_ = store.Put(auth.CachedToken{
		Token: "valid-token", Start: float64(time.Now().Unix()), Expire: float64(time.Now().Unix() + 3600),
		User: "saltadmin", EAuth: "pam", Profile: "test", URL: srv.URL,
	})
	client, err := New(&config.ResolvedProfile{
		Name: "test", URL: srv.URL, Username: "saltadmin", Password: "secret", EAuth: "pam", Timeout: 5 * time.Second,
	}, store, false)
	if err != nil {
		t.Fatalf("New client failed: %v", err)
	}
	return client
}

// jobServer serves /jobs/<jid> with the payload returned by respond for the n-th poll (1-based).
func jobServer(t *testing.T, jid string, polls *int32, respond func(n int32) map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/"+jid, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(respond(atomic.AddInt32(polls, 1)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// notFoundPayload mirrors salt-api's answer for an unknown or purged jid.
func notFoundPayload(jid string) map[string]any {
	return map[string]any{
		"info": []any{map[string]any{
			"Error": "Cannot contact returner or no job with this jid", "Result": map[string]any{},
			"StartTime": "2026, Oct 29 10:40:26.349943", "jid": jid,
		}},
		"return": []any{map[string]any{}},
	}
}

func TestWaitJobUnknownJidStops(t *testing.T) {
	const jid = "20261029104026349943"
	var polls int32
	srv := jobServer(t, jid, &polls, func(int32) map[string]any { return notFoundPayload(jid) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := newWaitTestClient(t, srv).WaitJob(ctx, jid, nil, 5*time.Millisecond)
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("want ErrJobNotFound, got %v", err)
	}
	if got := atomic.LoadInt32(&polls); got != jobNotFoundPolls {
		t.Errorf("want %d polls before giving up, got %d", jobNotFoundPolls, got)
	}
}

func TestWaitJobToleratesTransientNotFound(t *testing.T) {
	const jid = "20261104190000000001"
	var polls int32
	srv := jobServer(t, jid, &polls, func(n int32) map[string]any {
		if n <= 2 {
			return notFoundPayload(jid)
		}
		return map[string]any{
			"info":   []any{map[string]any{"jid": jid, "Minions": []any{"web-01"}}},
			"return": []any{map[string]any{"web-01": true}},
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := newWaitTestClient(t, srv).WaitJob(ctx, jid, []string{"web-01"}, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitJob: %v", err)
	}
	if res["web-01"] != true {
		t.Errorf("unexpected result %v", res)
	}
}

func TestWaitJobPendingHonoursContext(t *testing.T) {
	const jid = "20261104190000000002"
	var polls int32
	srv := jobServer(t, jid, &polls, func(int32) map[string]any {
		return map[string]any{
			"info":   []any{map[string]any{"jid": jid, "Minions": []any{"web-01", "web-02"}}},
			"return": []any{map[string]any{"web-01": true}},
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res, err := newWaitTestClient(t, srv).WaitJob(ctx, jid, nil, 5*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrJobNotFound) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	if res["web-01"] != true {
		t.Errorf("want partial results, got %v", res)
	}
}
