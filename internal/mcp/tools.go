package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/laurentL/salt-api-cli/internal/auth"
	"github.com/laurentL/salt-api-cli/internal/config"
	"github.com/laurentL/salt-api-cli/internal/saltclient"
)

// RegisterTools registers the 12 Full-Access Salt NetAPI tools on the MCP server.
func RegisterTools(s *MCPServer, baseOverrides config.Overrides, logger *log.Logger) {
	buildClient := func(req CallToolRequest) (*saltclient.Client, *config.ResolvedProfile, error) {
		ov := baseOverrides
		if prof := strings.TrimSpace(req.GetString("profile", "")); prof != "" {
			ov.Profile = prof
		}
		resolved, _, err := config.Resolve(ov)
		if err != nil {
			return nil, nil, err
		}
		store, err := auth.NewTokenStore("")
		if err != nil {
			return nil, nil, err
		}
		// Never prompt on stdin in MCP mode since stdin carries JSON-RPC frames!
		client, err := saltclient.New(resolved, store, false)
		if err != nil {
			return nil, nil, err
		}
		return client, resolved, nil
	}

	// 1. salt_list_profiles
	s.AddTool(
		NewTool("salt_list_profiles",
			WithDescription("List all configured Salt Master profiles from ~/.config/sapi/config.toml and show the default profile."),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			logger.Printf("Tool call: salt_list_profiles")
			cfg, err := config.Load(baseOverrides.ConfigPath)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			names := config.ProfileNames(cfg)
			sort.Strings(names)
			profiles := make(map[string]any, len(names))
			for _, name := range names {
				p := cfg.Profiles[name]
				verify := true
				if p.VerifySSL != nil {
					verify = *p.VerifySSL
				}
				profiles[name] = map[string]any{
					"url":        p.URL,
					"username":   p.Username,
					"eauth":      p.EAuth,
					"verify_ssl": verify,
					"is_default": name == cfg.DefaultProfile,
				}
			}
			return jsonToolResult(map[string]any{
				"default_profile": cfg.DefaultProfile,
				"profiles":        profiles,
			})
		},
	)

	// 2. salt_ping
	s.AddTool(
		NewTool("salt_ping",
			WithDescription("Test connectivity to Salt minions using test.ping."),
			WithString("tgt", Description("Target expression (default '*')"), DefaultString("*")),
			WithString("tgt_type", Description("Target matching type: glob, grain, pcre, list, pillar, compound, nodegroup, ipcidr"), DefaultString("glob")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			tgt := req.GetString("tgt", "*")
			tgtType := saltclient.TargetType(req.GetString("tgt_type", "glob"))
			logger.Printf("Tool call: salt_ping tgt=%s tgt_type=%s", tgt, tgtType)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			res, err := client.RunLocal(ctx, tgt, "test.ping", nil, nil, tgtType, 0)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 3. salt_run
	s.AddTool(
		NewTool("salt_run",
			WithDescription("Execute any Salt execution module on targeted minions (supports local, local_async with optional wait, and local_batch)."),
			WithString("tgt", Required(), Description("Target expression (e.g. '*', 'web-*', 'os:Debian')")),
			WithString("fun", Required(), Description("Salt execution module function (e.g. 'cmd.run', 'pkg.install', 'service.status')")),
			WithArray("args", WithStringItems(), Description("Optional positional arguments (key=value items are parsed into kwargs automatically)")),
			WithObject("kwargs", Description("Optional keyword arguments dictionary")),
			WithString("tgt_type", Description("Target type: glob, grain, grain_pcre, pcre, list, pillar, compound, nodegroup, ipcidr"), DefaultString("glob")),
			WithString("batch", Description("Optional batch size or percentage (e.g. '10%' or '5') to use local_batch")),
			WithBoolean("async", Description("Run asynchronously via local_async")),
			WithBoolean("wait", Description("If async=true, poll /jobs/<jid> until all minions return")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			tgt, err := req.RequireString("tgt")
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			fun, err := req.RequireString("fun")
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			rawArgs := req.GetStringSlice("args", nil)
			posArgs, kwargs := saltclient.ParseArgsAndKwargs(rawArgs, false)
			if extraKw, ok := req.GetArguments()["kwargs"].(map[string]any); ok && len(extraKw) > 0 {
				if kwargs == nil {
					kwargs = make(map[string]any)
				}
				for k, v := range extraKw {
					kwargs[k] = v
				}
			}

			tgtType := saltclient.TargetType(req.GetString("tgt_type", "glob"))
			batch := strings.TrimSpace(req.GetString("batch", ""))
			isAsync := req.GetBool("async", false)
			wait := req.GetBool("wait", false)
			logger.Printf("Tool call: salt_run tgt=%s fun=%s tgt_type=%s async=%v batch=%s", tgt, fun, tgtType, isAsync, batch)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}

			if batch != "" {
				res, err := client.RunLocalBatch(ctx, tgt, fun, batch, posArgs, kwargs, tgtType)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}

			if isAsync || wait {
				asyncRes, err := client.RunLocalAsync(ctx, tgt, fun, posArgs, kwargs, tgtType, 0)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				if !wait {
					return jsonToolResult(asyncRes)
				}
				waitRes, err := client.WaitJob(ctx, asyncRes.JID, asyncRes.Minions, 1500*time.Millisecond)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(map[string]any{
					"jid":    asyncRes.JID,
					"result": waitRes,
				})
			}

			res, err := client.RunLocal(ctx, tgt, fun, posArgs, kwargs, tgtType, 0)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 4. salt_state_apply
	s.AddTool(
		NewTool("salt_state_apply",
			WithDescription("Apply a Salt state (or highstate if state is omitted) on targeted minions."),
			WithString("tgt", Required(), Description("Target expression (e.g. '*', 'db-*')")),
			WithString("state", Description("State SLS name (leave empty to run full state.highstate)")),
			WithBoolean("test", Description("Run in dry-run test mode (test=True) without making actual changes")),
			WithObject("pillar", Description("Optional inline pillar dictionary override")),
			WithString("saltenv", Description("Optional Salt environment (e.g. 'base', 'prod')")),
			WithString("tgt_type", Description("Target matching type (default 'glob')"), DefaultString("glob")),
			WithBoolean("wait", Description("Run via local_async and wait for completion")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			tgt, err := req.RequireString("tgt")
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			stateSLS := strings.TrimSpace(req.GetString("state", ""))
			testMode := req.GetBool("test", false)
			saltenv := strings.TrimSpace(req.GetString("saltenv", ""))
			tgtType := saltclient.TargetType(req.GetString("tgt_type", "glob"))
			wait := req.GetBool("wait", false)

			var posArgs []any
			if stateSLS != "" {
				posArgs = append(posArgs, stateSLS)
			}
			kwargs := make(map[string]any)
			if testMode {
				kwargs["test"] = true
			}
			if saltenv != "" {
				kwargs["saltenv"] = saltenv
			}
			if pillar, ok := req.GetArguments()["pillar"].(map[string]any); ok && len(pillar) > 0 {
				kwargs["pillar"] = pillar
			}

			logger.Printf("Tool call: salt_state_apply tgt=%s state=%s test=%v", tgt, stateSLS, testMode)
			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}

			if wait {
				asyncRes, err := client.RunLocalAsync(ctx, tgt, "state.apply", posArgs, kwargs, tgtType, 0)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				res, err := client.WaitJob(ctx, asyncRes.JID, asyncRes.Minions, 1500*time.Millisecond)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(map[string]any{"jid": asyncRes.JID, "result": res})
			}

			res, err := client.RunLocal(ctx, tgt, "state.apply", posArgs, kwargs, tgtType, 0)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 5. salt_list_minions
	s.AddTool(
		NewTool("salt_list_minions",
			WithDescription("List known Salt minions and their inventory summary (or check up/down status via manage.status)."),
			WithBoolean("check_status", Description("If true, runs Master runner manage.status to report up/down minions instead of /minions")),
			WithBoolean("full_grains", Description("If true, returns all grains for every minion instead of a compact summary")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			checkStatus := req.GetBool("check_status", false)
			fullGrains := req.GetBool("full_grains", false)
			logger.Printf("Tool call: salt_list_minions check_status=%v full_grains=%v", checkStatus, fullGrains)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}

			if checkStatus {
				res, err := client.RunRunner(ctx, "manage.status", nil, nil)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}

			minions, err := client.ListMinions(ctx)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			if fullGrains {
				return jsonToolResult(minions)
			}

			summary := make(map[string]any, len(minions))
			for id, raw := range minions {
				gm, ok := raw.(map[string]any)
				if !ok {
					summary[id] = raw
					continue
				}
				info := map[string]any{}
				for _, k := range []string{"osfinger", "os", "osrelease", "ipv4", "fqdn", "saltversion", "kernelrelease"} {
					if v, exists := gm[k]; exists {
						info[k] = v
					}
				}
				summary[id] = info
			}
			return jsonToolResult(summary)
		},
	)

	// 6. salt_get_minion_grains
	s.AddTool(
		NewTool("salt_get_minion_grains",
			WithDescription("Retrieve all or specific Salt grains for a single minion via /minions/<minion_id>."),
			WithString("minion_id", Required(), Description("Exact minion ID")),
			WithArray("keys", WithStringItems(), Description("Optional list of specific grain names to extract (e.g. ['os', 'ipv4', 'mem_total'])")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			minionID, err := req.RequireString("minion_id")
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			keys := req.GetStringSlice("keys", nil)
			logger.Printf("Tool call: salt_get_minion_grains minion_id=%s keys=%v", minionID, keys)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			grains, err := client.GetMinion(ctx, minionID)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			if len(keys) > 0 {
				if gm, ok := grains.(map[string]any); ok {
					filtered := make(map[string]any, len(keys))
					for _, k := range keys {
						filtered[k] = gm[k]
					}
					return jsonToolResult(filtered)
				}
			}
			return jsonToolResult(grains)
		},
	)

	// 7. salt_list_jobs
	s.AddTool(
		NewTool("salt_list_jobs",
			WithDescription("List recent Salt jobs from the Master cache (/jobs) or currently running jobs (jobs.active)."),
			WithBoolean("active_only", Description("If true, returns only currently running jobs via runner jobs.active")),
			WithNumber("limit", Description("Maximum number of recent jobs to return (default 25)")),
			WithString("function_filter", Description("Optional substring filter on the Salt function name")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			activeOnly := req.GetBool("active_only", false)
			limit := req.GetInt("limit", 25)
			funFilter := strings.TrimSpace(req.GetString("function_filter", ""))
			logger.Printf("Tool call: salt_list_jobs active_only=%v limit=%d filter=%s", activeOnly, limit, funFilter)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			if activeOnly {
				res, err := client.RunRunner(ctx, "jobs.active", nil, nil)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}

			jobs, err := client.ListJobs(ctx)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			jids := make([]string, 0, len(jobs))
			for jid, meta := range jobs {
				if funFilter != "" {
					if m, ok := meta.(map[string]any); ok {
						fn, _ := m["Function"].(string)
						if !strings.Contains(strings.ToLower(fn), strings.ToLower(funFilter)) {
							continue
						}
					}
				}
				jids = append(jids, jid)
			}
			sort.Sort(sort.Reverse(sort.StringSlice(jids)))
			if limit > 0 && len(jids) > limit {
				jids = jids[:limit]
			}
			out := make(map[string]any, len(jids))
			for _, jid := range jids {
				out[jid] = jobs[jid]
			}
			return jsonToolResult(out)
		},
	)

	// 8. salt_get_job
	s.AddTool(
		NewTool("salt_get_job",
			WithDescription("Inspect the execution metadata and minion return values for a Salt job (/jobs/<jid>)."),
			WithString("jid", Required(), Description("Salt Job ID (JID)")),
			WithBoolean("wait", Description("If true, polls until all targeted minions finish before returning")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			jid, err := req.RequireString("jid")
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			wait := req.GetBool("wait", false)
			logger.Printf("Tool call: salt_get_job jid=%s wait=%v", jid, wait)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			if wait {
				res, err := client.WaitJob(ctx, jid, nil, 1500*time.Millisecond)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}
			resp, err := client.GetJob(ctx, jid)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(resp)
		},
	)

	// 9. salt_runner
	s.AddTool(
		NewTool("salt_runner",
			WithDescription("Execute any Salt Master runner module (e.g. 'manage.status', 'state.orchestrate', 'fileserver.envs')."),
			WithString("fun", Required(), Description("Runner function name")),
			WithArray("args", WithStringItems(), Description("Optional positional arguments")),
			WithObject("kwargs", Description("Optional keyword arguments dictionary")),
			WithBoolean("async", Description("Execute asynchronously via runner_async")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			fun, err := req.RequireString("fun")
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			rawArgs := req.GetStringSlice("args", nil)
			posArgs, kwargs := saltclient.ParseArgsAndKwargs(rawArgs, false)
			if extraKw, ok := req.GetArguments()["kwargs"].(map[string]any); ok && len(extraKw) > 0 {
				if kwargs == nil {
					kwargs = make(map[string]any)
				}
				for k, v := range extraKw {
					kwargs[k] = v
				}
			}
			isAsync := req.GetBool("async", false)
			logger.Printf("Tool call: salt_runner fun=%s async=%v", fun, isAsync)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			if isAsync {
				res, err := client.RunRunnerAsync(ctx, fun, posArgs, kwargs)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}
			res, err := client.RunRunner(ctx, fun, posArgs, kwargs)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 10. salt_wheel
	s.AddTool(
		NewTool("salt_wheel",
			WithDescription("Execute any Salt Master wheel module function (e.g. 'key.list_all', 'config.values')."),
			WithString("fun", Required(), Description("Wheel function name")),
			WithArray("args", WithStringItems(), Description("Optional positional arguments")),
			WithObject("kwargs", Description("Optional keyword arguments dictionary")),
			WithBoolean("async", Description("Execute asynchronously via wheel_async")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			fun, err := req.RequireString("fun")
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			rawArgs := req.GetStringSlice("args", nil)
			posArgs, kwargs := saltclient.ParseArgsAndKwargs(rawArgs, false)
			if extraKw, ok := req.GetArguments()["kwargs"].(map[string]any); ok && len(extraKw) > 0 {
				if kwargs == nil {
					kwargs = make(map[string]any)
				}
				for k, v := range extraKw {
					kwargs[k] = v
				}
			}
			isAsync := req.GetBool("async", false)
			logger.Printf("Tool call: salt_wheel fun=%s async=%v", fun, isAsync)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			if isAsync {
				res, err := client.RunWheelAsync(ctx, fun, posArgs, kwargs)
				if err != nil {
					return NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}
			res, err := client.RunWheel(ctx, fun, posArgs, kwargs)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 11. salt_keys_manage
	s.AddTool(
		NewTool("salt_keys_manage",
			WithDescription("Manage Salt minion keys on the Master (list, accept, reject, delete, finger) via wheel.key."),
			WithString("action", Required(), Enum("list", "accept", "reject", "delete", "finger"), Description("Key management action: list, accept, reject, delete, finger")),
			WithString("match", Description("Minion ID or glob pattern for accept, reject, delete, or finger (e.g. 'web-01' or '*')")),
			WithBoolean("include_accepted", Description("When rejecting keys, also include accepted keys")),
			WithBoolean("include_rejected", Description("When accepting keys, also include rejected keys")),
			WithBoolean("include_denied", Description("When accepting or rejecting keys, also include denied keys")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			action, err := req.RequireString("action")
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			match := strings.TrimSpace(req.GetString("match", ""))
			logger.Printf("Tool call: salt_keys_manage action=%s match=%s", action, match)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}

			var res any
			switch action {
			case "list":
				res, err = client.ListKeys(ctx)
			case "accept":
				if match == "" {
					return NewToolResultError("match is required for action='accept'"), nil
				}
				res, err = client.AcceptKey(ctx, match, req.GetBool("include_rejected", false), req.GetBool("include_denied", false))
			case "reject":
				if match == "" {
					return NewToolResultError("match is required for action='reject'"), nil
				}
				res, err = client.RejectKey(ctx, match, req.GetBool("include_accepted", false), req.GetBool("include_denied", false))
			case "delete":
				if match == "" {
					return NewToolResultError("match is required for action='delete'"), nil
				}
				res, err = client.DeleteKey(ctx, match)
			case "finger":
				res, err = client.KeyFinger(ctx, match)
			default:
				return NewToolResultError(fmt.Sprintf("unsupported action %q", action)), nil
			}
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 12. salt_send_hook
	s.AddTool(
		NewTool("salt_send_hook",
			WithDescription("Trigger a Salt NetAPI webhook (POST /hook/<tag>) to fire Salt Master reactor events."),
			WithString("tag", Required(), Description("Webhook tag path (e.g. 'deploy/app')")),
			WithObject("payload", Description("JSON dictionary payload sent to the webhook")),
			WithString("profile", Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			tag, err := req.RequireString("tag")
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			payload, _ := req.GetArguments()["payload"].(map[string]any)
			logger.Printf("Tool call: salt_send_hook tag=%s", tag)

			client, _, err := buildClient(req)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			res, err := client.SendWebhook(ctx, tag, payload)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)
}

func jsonToolResult(data any) (*CallToolResult, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return NewToolResultError(fmt.Sprintf("failed to serialize result: %v", err)), nil
	}
	return NewToolResultText(string(b)), nil
}
