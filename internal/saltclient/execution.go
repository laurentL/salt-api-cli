package saltclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Execute sends one LowState to POST / and returns the first element of the "return" array.
func (c *Client) Execute(ctx context.Context, state LowState) (any, error) {
	if state.TgtType == TargetList {
		if s, ok := state.Tgt.(string); ok {
			parts := strings.Split(s, ",")
			cleaned := make([]string, 0, len(parts))
			for _, p := range parts {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					cleaned = append(cleaned, trimmed)
				}
			}
			state.Tgt = cleaned
		}
	}

	var env RawEnvelope
	_, err := c.doJSON(ctx, "POST", "/", []LowState{state}, &env)
	if err != nil {
		return nil, err
	}
	if len(env.Return) == 0 {
		return map[string]any{}, nil
	}
	if len(env.Return) == 1 {
		var result any
		if err := json.Unmarshal(env.Return[0], &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal return payload: %w", err)
		}
		return result, nil
	}
	results := make([]any, 0, len(env.Return))
	for _, raw := range env.Return {
		var item any
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, nil
}

// RunLocal executes a synchronous Salt module on targeted minions (client=local).
func (c *Client) RunLocal(ctx context.Context, tgt string, fun string, args []any, kwargs map[string]any, tgtType TargetType, timeoutSec int) (any, error) {
	if tgtType == "" {
		tgtType = TargetGlob
	}
	state := LowState{
		Client:  ClientLocal,
		Tgt:     tgt,
		Fun:     fun,
		Arg:     args,
		Kwarg:   kwargs,
		TgtType: tgtType,
		Timeout: timeoutSec,
	}
	return c.Execute(ctx, state)
}

// RunLocalAsync starts an asynchronous Salt job on targeted minions (client=local_async).
func (c *Client) RunLocalAsync(ctx context.Context, tgt string, fun string, args []any, kwargs map[string]any, tgtType TargetType, timeoutSec int) (*AsyncResult, error) {
	if tgtType == "" {
		tgtType = TargetGlob
	}
	state := LowState{
		Client:  ClientLocalAsync,
		Tgt:     tgt,
		Fun:     fun,
		Arg:     args,
		Kwarg:   kwargs,
		TgtType: tgtType,
		Timeout: timeoutSec,
	}
	var env RawEnvelope
	_, err := c.doJSON(ctx, "POST", "/", []LowState{state}, &env)
	if err != nil {
		return nil, err
	}
	if len(env.Return) == 0 {
		return nil, errors.New("salt-api returned empty response for local_async")
	}
	var res AsyncResult
	if err := json.Unmarshal(env.Return[0], &res); err != nil {
		return nil, fmt.Errorf("failed to parse local_async response: %w", err)
	}
	return &res, nil
}

// RunLocalBatch executes a Salt module in batches across targeted minions (client=local_batch).
func (c *Client) RunLocalBatch(ctx context.Context, tgt string, fun string, batch string, args []any, kwargs map[string]any, tgtType TargetType) (any, error) {
	if tgtType == "" {
		tgtType = TargetGlob
	}
	if batch == "" {
		batch = "10%"
	}
	state := LowState{
		Client:  ClientLocalBatch,
		Tgt:     tgt,
		Fun:     fun,
		Batch:   batch,
		Arg:     args,
		Kwarg:   kwargs,
		TgtType: tgtType,
	}
	return c.Execute(ctx, state)
}

// RunRunner executes a Salt Master runner synchronously (client=runner).
func (c *Client) RunRunner(ctx context.Context, fun string, args []any, kwargs map[string]any) (any, error) {
	state := LowState{
		Client: ClientRunner,
		Fun:    fun,
		Arg:    args,
		Kwarg:  kwargs,
	}
	return c.Execute(ctx, state)
}

// RunRunnerAsync executes a Salt Master runner asynchronously (client=runner_async).
func (c *Client) RunRunnerAsync(ctx context.Context, fun string, args []any, kwargs map[string]any) (*AsyncResult, error) {
	state := LowState{
		Client: ClientRunnerAsync,
		Fun:    fun,
		Arg:    args,
		Kwarg:  kwargs,
	}
	var env RawEnvelope
	_, err := c.doJSON(ctx, "POST", "/", []LowState{state}, &env)
	if err != nil {
		return nil, err
	}
	if len(env.Return) == 0 {
		return nil, errors.New("salt-api returned empty response for runner_async")
	}
	var res AsyncResult
	if err := json.Unmarshal(env.Return[0], &res); err != nil {
		return nil, fmt.Errorf("failed to parse runner_async response: %w", err)
	}
	return &res, nil
}

// RunWheel executes a Salt Master wheel function synchronously (client=wheel).
// If the wheel response wraps data inside {"data": {"return": ...}}, it extracts the inner return value.
func (c *Client) RunWheel(ctx context.Context, fun string, args []any, kwargs map[string]any) (any, error) {
	state := LowState{
		Client: ClientWheel,
		Fun:    fun,
		Arg:    args,
		Kwarg:  kwargs,
	}
	res, err := c.Execute(ctx, state)
	if err != nil {
		return nil, err
	}
	return unwrapWheelReturn(res), nil
}

// RunWheelAsync executes a Salt Master wheel function asynchronously (client=wheel_async).
func (c *Client) RunWheelAsync(ctx context.Context, fun string, args []any, kwargs map[string]any) (*AsyncResult, error) {
	state := LowState{
		Client: ClientWheelAsync,
		Fun:    fun,
		Arg:    args,
		Kwarg:  kwargs,
	}
	var env RawEnvelope
	_, err := c.doJSON(ctx, "POST", "/", []LowState{state}, &env)
	if err != nil {
		return nil, err
	}
	if len(env.Return) == 0 {
		return nil, errors.New("salt-api returned empty response for wheel_async")
	}
	var res AsyncResult
	if err := json.Unmarshal(env.Return[0], &res); err != nil {
		return nil, fmt.Errorf("failed to parse wheel_async response: %w", err)
	}
	return &res, nil
}

// ListKeys retrieves all minion keys grouped by status via wheel key.list_all.
func (c *Client) ListKeys(ctx context.Context) (any, error) {
	return c.RunWheel(ctx, "key.list_all", nil, nil)
}

// AcceptKey accepts minion keys matching match via wheel key.accept.
func (c *Client) AcceptKey(ctx context.Context, match string, includeRejected, includeDenied bool) (any, error) {
	kwargs := map[string]any{
		"match": match,
	}
	if includeRejected {
		kwargs["include_rejected"] = true
	}
	if includeDenied {
		kwargs["include_denied"] = true
	}
	return c.RunWheel(ctx, "key.accept", nil, kwargs)
}

// RejectKey rejects minion keys matching match via wheel key.reject.
func (c *Client) RejectKey(ctx context.Context, match string, includeAccepted, includeDenied bool) (any, error) {
	kwargs := map[string]any{
		"match": match,
	}
	if includeAccepted {
		kwargs["include_accepted"] = true
	}
	if includeDenied {
		kwargs["include_denied"] = true
	}
	return c.RunWheel(ctx, "key.reject", nil, kwargs)
}

// DeleteKey deletes minion keys matching match via wheel key.delete.
func (c *Client) DeleteKey(ctx context.Context, match string) (any, error) {
	kwargs := map[string]any{
		"match": match,
	}
	return c.RunWheel(ctx, "key.delete", nil, kwargs)
}

// KeyFinger retrieves key fingerprints matching match via wheel key.finger.
func (c *Client) KeyFinger(ctx context.Context, match string) (any, error) {
	if match == "" {
		match = "*"
	}
	kwargs := map[string]any{
		"match": match,
	}
	return c.RunWheel(ctx, "key.finger", nil, kwargs)
}

func unwrapWheelReturn(res any) any {
	m, ok := res.(map[string]any)
	if !ok {
		return res
	}
	if data, ok := m["data"].(map[string]any); ok {
		if inner, hasRet := data["return"]; hasRet {
			return inner
		}
	}
	return res
}
