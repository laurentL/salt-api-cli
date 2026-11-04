package saltclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// JobLookupResponse models the response of GET /jobs/<jid> in salt-api.
type JobLookupResponse struct {
	Info   []map[string]any `json:"info,omitempty"`
	Return []map[string]any `json:"return"`
}

// ListJobs calls GET /jobs and returns the map of JID -> job metadata.
func (c *Client) ListJobs(ctx context.Context) (map[string]any, error) {
	var env RawEnvelope
	_, err := c.doJSON(ctx, "GET", "/jobs", nil, &env)
	if err != nil {
		return nil, err
	}
	if len(env.Return) == 0 {
		return map[string]any{}, nil
	}
	var jobs map[string]any
	if err := json.Unmarshal(env.Return[0], &jobs); err != nil {
		return nil, fmt.Errorf("failed to parse /jobs response: %w", err)
	}
	return jobs, nil
}

// GetJob calls GET /jobs/<jid> and returns the full JobLookupResponse (both info and return).
func (c *Client) GetJob(ctx context.Context, jid string) (*JobLookupResponse, error) {
	jid = strings.TrimSpace(jid)
	if jid == "" {
		return nil, fmt.Errorf("job ID (jid) cannot be empty")
	}
	var resp JobLookupResponse
	_, err := c.doJSON(ctx, "GET", "/jobs/"+jid, nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ErrJobNotFound is returned by WaitJob when the master keeps answering that it has no job with this jid
// (unknown jid, or a job already purged from the job cache by keep_jobs).
var ErrJobNotFound = errors.New("job not found")

// jobNotFoundPolls is the number of consecutive "no job with this jid" answers WaitJob tolerates before
// giving up. The grace covers a lookup racing the job cache write right after local_async.
const jobNotFoundPolls = 5

// jobLookupError returns the error reported by the master in the job info (e.g. "Cannot contact returner
// or no job with this jid"), or "" when the job is known.
func jobLookupError(resp *JobLookupResponse) string {
	for _, info := range resp.Info {
		if msg, ok := info["Error"].(string); ok && msg != "" {
			return msg
		}
	}
	return ""
}

// WaitJob polls GET /jobs/<jid> until all expected minions have returned results or ctx expires.
// It fails with ErrJobNotFound when the master keeps reporting that the job does not exist, so
// callers never poll forever on an unknown or purged jid. Callers should still bound ctx.
func (c *Client) WaitJob(ctx context.Context, jid string, expectedMinions []string, pollInterval time.Duration) (map[string]any, error) {
	if pollInterval <= 0 {
		pollInterval = 1500 * time.Millisecond
	}

	expectedSet := make(map[string]struct{}, len(expectedMinions))
	for _, m := range expectedMinions {
		if m != "" {
			expectedSet[m] = struct{}{}
		}
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastResult map[string]any
	notFound := 0

	for {
		jobResp, err := c.GetJob(ctx, jid)
		if err == nil && jobResp != nil {
			hasReturn := len(jobResp.Return) > 0 && len(jobResp.Return[0]) > 0
			if msg := jobLookupError(jobResp); msg != "" && !hasReturn {
				notFound++
				if notFound >= jobNotFoundPolls {
					return nil, fmt.Errorf("%w: %s (%s)", ErrJobNotFound, jid, msg)
				}
			} else {
				notFound = 0
			}

			// Populate expected minions from job info if not known initially
			if len(expectedSet) == 0 && len(jobResp.Info) > 0 {
				if minionsRaw, ok := jobResp.Info[0]["Minions"].([]any); ok {
					for _, mr := range minionsRaw {
						if ms, ok := mr.(string); ok && ms != "" {
							expectedSet[ms] = struct{}{}
						}
					}
				}
			}

			if len(jobResp.Return) > 0 && jobResp.Return[0] != nil {
				lastResult = jobResp.Return[0]
				if len(expectedSet) > 0 {
					allDone := true
					for m := range expectedSet {
						if _, has := lastResult[m]; !has {
							allDone = false
							break
						}
					}
					if allDone {
						return lastResult, nil
					}
				} else if len(lastResult) > 0 {
					return lastResult, nil
				}
			}
		}

		select {
		case <-ctx.Done():
			if lastResult != nil && len(lastResult) > 0 {
				return lastResult, fmt.Errorf("timed out waiting for all minions on job %s (partial results returned): %w", jid, ctx.Err())
			}
			return nil, fmt.Errorf("timed out waiting for job %s: %w", jid, ctx.Err())
		case <-ticker.C:
		}
	}
}

// ListMinions calls GET /minions and returns all minions and their grains.
func (c *Client) ListMinions(ctx context.Context) (map[string]any, error) {
	var env RawEnvelope
	_, err := c.doJSON(ctx, "GET", "/minions", nil, &env)
	if err != nil {
		return nil, err
	}
	if len(env.Return) == 0 {
		return map[string]any{}, nil
	}
	var minions map[string]any
	if err := json.Unmarshal(env.Return[0], &minions); err != nil {
		return nil, fmt.Errorf("failed to parse /minions response: %w", err)
	}
	return minions, nil
}

// GetMinion calls GET /minions/<mid> and returns the grains for a specific minion.
func (c *Client) GetMinion(ctx context.Context, minionID string) (any, error) {
	minionID = strings.TrimSpace(minionID)
	if minionID == "" {
		return nil, fmt.Errorf("minion ID cannot be empty")
	}
	var env RawEnvelope
	_, err := c.doJSON(ctx, "GET", "/minions/"+minionID, nil, &env)
	if err != nil {
		return nil, err
	}
	if len(env.Return) == 0 {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := json.Unmarshal(env.Return[0], &data); err != nil {
		return nil, fmt.Errorf("failed to parse /minions/%s response: %w", minionID, err)
	}
	if val, ok := data[minionID]; ok {
		return val, nil
	}
	return data, nil
}

// Stats calls GET /stats and returns salt-api server statistics.
func (c *Client) Stats(ctx context.Context) (any, error) {
	var stats any
	_, err := c.doJSON(ctx, "GET", "/stats", nil, &stats)
	if err != nil {
		return nil, err
	}
	return stats, nil
}
