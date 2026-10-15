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
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterTools registers the 12 Full-Access Salt NetAPI tools on the MCP server.
func RegisterTools(s *server.MCPServer, baseOverrides config.Overrides, logger *log.Logger) {
	buildClient := func(req mcp.CallToolRequest) (*saltclient.Client, *config.ResolvedProfile, error) {
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
		mcp.NewTool("salt_list_profiles",
			mcp.WithDescription("List all configured Salt Master profiles from ~/.config/sapi/config.toml and show the default profile."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			logger.Printf("Tool call: salt_list_profiles")
			cfg, err := config.Load(baseOverrides.ConfigPath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
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
		mcp.NewTool("salt_ping",
			mcp.WithDescription("Test connectivity to Salt minions using test.ping."),
			mcp.WithString("tgt", mcp.Description("Target expression (default '*')"), mcp.DefaultString("*")),
			mcp.WithString("tgt_type", mcp.Description("Target matching type: glob, grain, pcre, list, pillar, compound, nodegroup, ipcidr"), mcp.DefaultString("glob")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tgt := req.GetString("tgt", "*")
			tgtType := saltclient.TargetType(req.GetString("tgt_type", "glob"))
			logger.Printf("Tool call: salt_ping tgt=%s tgt_type=%s", tgt, tgtType)

			client, _, err := buildClient(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, err := client.RunLocal(ctx, tgt, "test.ping", nil, nil, tgtType, 0)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 3. salt_run
	s.AddTool(
		mcp.NewTool("salt_run",
			mcp.WithDescription("Execute any Salt execution module on targeted minions (supports local, local_async with optional wait, and local_batch)."),
			mcp.WithString("tgt", mcp.Required(), mcp.Description("Target expression (e.g. '*', 'web-*', 'os:Debian')")),
			mcp.WithString("fun", mcp.Required(), mcp.Description("Salt execution module function (e.g. 'cmd.run', 'pkg.install', 'service.status')")),
			mcp.WithArray("args", mcp.WithStringItems(), mcp.Description("Optional positional arguments (key=value items are parsed into kwargs automatically)")),
			mcp.WithObject("kwargs", mcp.Description("Optional keyword arguments dictionary")),
			mcp.WithString("tgt_type", mcp.Description("Target type: glob, grain, grain_pcre, pcre, list, pillar, compound, nodegroup, ipcidr"), mcp.DefaultString("glob")),
			mcp.WithString("batch", mcp.Description("Optional batch size or percentage (e.g. '10%' or '5') to use local_batch")),
			mcp.WithBoolean("async", mcp.Description("Run asynchronously via local_async")),
			mcp.WithBoolean("wait", mcp.Description("If async=true, poll /jobs/<jid> until all minions return")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tgt, err := req.RequireString("tgt")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			fun, err := req.RequireString("fun")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
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
				return mcp.NewToolResultError(err.Error()), nil
			}

			if batch != "" {
				res, err := client.RunLocalBatch(ctx, tgt, fun, batch, posArgs, kwargs, tgtType)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}

			if isAsync || wait {
				asyncRes, err := client.RunLocalAsync(ctx, tgt, fun, posArgs, kwargs, tgtType, 0)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				if !wait {
					return jsonToolResult(asyncRes)
				}
				waitRes, err := client.WaitJob(ctx, asyncRes.JID, asyncRes.Minions, 1500*time.Millisecond)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(map[string]any{
					"jid":    asyncRes.JID,
					"result": waitRes,
				})
			}

			res, err := client.RunLocal(ctx, tgt, fun, posArgs, kwargs, tgtType, 0)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 4. salt_state_apply
	s.AddTool(
		mcp.NewTool("salt_state_apply",
			mcp.WithDescription("Apply a Salt state (or highstate if state is omitted) on targeted minions."),
			mcp.WithString("tgt", mcp.Required(), mcp.Description("Target expression (e.g. '*', 'db-*')")),
			mcp.WithString("state", mcp.Description("State SLS name (leave empty to run full state.highstate)")),
			mcp.WithBoolean("test", mcp.Description("Run in dry-run test mode (test=True) without making actual changes")),
			mcp.WithObject("pillar", mcp.Description("Optional inline pillar dictionary override")),
			mcp.WithString("saltenv", mcp.Description("Optional Salt environment (e.g. 'base', 'prod')")),
			mcp.WithString("tgt_type", mcp.Description("Target matching type (default 'glob')"), mcp.DefaultString("glob")),
			mcp.WithBoolean("wait", mcp.Description("Run via local_async and wait for completion")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tgt, err := req.RequireString("tgt")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
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
				return mcp.NewToolResultError(err.Error()), nil
			}

			if wait {
				asyncRes, err := client.RunLocalAsync(ctx, tgt, "state.apply", posArgs, kwargs, tgtType, 0)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				res, err := client.WaitJob(ctx, asyncRes.JID, asyncRes.Minions, 1500*time.Millisecond)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(map[string]any{"jid": asyncRes.JID, "result": res})
			}

			res, err := client.RunLocal(ctx, tgt, "state.apply", posArgs, kwargs, tgtType, 0)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 5. salt_list_minions
	s.AddTool(
		mcp.NewTool("salt_list_minions",
			mcp.WithDescription("List known Salt minions and their inventory summary (or check up/down status via manage.status)."),
			mcp.WithBoolean("check_status", mcp.Description("If true, runs Master runner manage.status to report up/down minions instead of /minions")),
			mcp.WithBoolean("full_grains", mcp.Description("If true, returns all grains for every minion instead of a compact summary")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			checkStatus := req.GetBool("check_status", false)
			fullGrains := req.GetBool("full_grains", false)
			logger.Printf("Tool call: salt_list_minions check_status=%v full_grains=%v", checkStatus, fullGrains)

			client, _, err := buildClient(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if checkStatus {
				res, err := client.RunRunner(ctx, "manage.status", nil, nil)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}

			minions, err := client.ListMinions(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
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
		mcp.NewTool("salt_get_minion_grains",
			mcp.WithDescription("Retrieve all or specific Salt grains for a single minion via /minions/<minion_id>."),
			mcp.WithString("minion_id", mcp.Required(), mcp.Description("Exact minion ID")),
			mcp.WithArray("keys", mcp.WithStringItems(), mcp.Description("Optional list of specific grain names to extract (e.g. ['os', 'ipv4', 'mem_total'])")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			minionID, err := req.RequireString("minion_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			keys := req.GetStringSlice("keys", nil)
			logger.Printf("Tool call: salt_get_minion_grains minion_id=%s keys=%v", minionID, keys)

			client, _, err := buildClient(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			grains, err := client.GetMinion(ctx, minionID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
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
		mcp.NewTool("salt_list_jobs",
			mcp.WithDescription("List recent Salt jobs from the Master cache (/jobs) or currently running jobs (jobs.active)."),
			mcp.WithBoolean("active_only", mcp.Description("If true, returns only currently running jobs via runner jobs.active")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of recent jobs to return (default 25)")),
			mcp.WithString("function_filter", mcp.Description("Optional substring filter on the Salt function name")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			activeOnly := req.GetBool("active_only", false)
			limit := req.GetInt("limit", 25)
			funFilter := strings.TrimSpace(req.GetString("function_filter", ""))
			logger.Printf("Tool call: salt_list_jobs active_only=%v limit=%d filter=%s", activeOnly, limit, funFilter)

			client, _, err := buildClient(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if activeOnly {
				res, err := client.RunRunner(ctx, "jobs.active", nil, nil)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}

			jobs, err := client.ListJobs(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
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
		mcp.NewTool("salt_get_job",
			mcp.WithDescription("Inspect the execution metadata and minion return values for a Salt job (/jobs/<jid>)."),
			mcp.WithString("jid", mcp.Required(), mcp.Description("Salt Job ID (JID)")),
			mcp.WithBoolean("wait", mcp.Description("If true, polls until all targeted minions finish before returning")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			jid, err := req.RequireString("jid")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			wait := req.GetBool("wait", false)
			logger.Printf("Tool call: salt_get_job jid=%s wait=%v", jid, wait)

			client, _, err := buildClient(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if wait {
				res, err := client.WaitJob(ctx, jid, nil, 1500*time.Millisecond)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}
			resp, err := client.GetJob(ctx, jid)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(resp)
		},
	)

	// 9. salt_runner
	s.AddTool(
		mcp.NewTool("salt_runner",
			mcp.WithDescription("Execute any Salt Master runner module (e.g. 'manage.status', 'state.orchestrate', 'fileserver.envs')."),
			mcp.WithString("fun", mcp.Required(), mcp.Description("Runner function name")),
			mcp.WithArray("args", mcp.WithStringItems(), mcp.Description("Optional positional arguments")),
			mcp.WithObject("kwargs", mcp.Description("Optional keyword arguments dictionary")),
			mcp.WithBoolean("async", mcp.Description("Execute asynchronously via runner_async")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			fun, err := req.RequireString("fun")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
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
				return mcp.NewToolResultError(err.Error()), nil
			}
			if isAsync {
				res, err := client.RunRunnerAsync(ctx, fun, posArgs, kwargs)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}
			res, err := client.RunRunner(ctx, fun, posArgs, kwargs)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 10. salt_wheel
	s.AddTool(
		mcp.NewTool("salt_wheel",
			mcp.WithDescription("Execute any Salt Master wheel module function (e.g. 'key.list_all', 'config.values')."),
			mcp.WithString("fun", mcp.Required(), mcp.Description("Wheel function name")),
			mcp.WithArray("args", mcp.WithStringItems(), mcp.Description("Optional positional arguments")),
			mcp.WithObject("kwargs", mcp.Description("Optional keyword arguments dictionary")),
			mcp.WithBoolean("async", mcp.Description("Execute asynchronously via wheel_async")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			fun, err := req.RequireString("fun")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
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
				return mcp.NewToolResultError(err.Error()), nil
			}
			if isAsync {
				res, err := client.RunWheelAsync(ctx, fun, posArgs, kwargs)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return jsonToolResult(res)
			}
			res, err := client.RunWheel(ctx, fun, posArgs, kwargs)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 11. salt_keys_manage
	s.AddTool(
		mcp.NewTool("salt_keys_manage",
			mcp.WithDescription("Manage Salt minion keys on the Master (list, accept, reject, delete, finger) via wheel.key."),
			mcp.WithString("action", mcp.Required(), mcp.Enum("list", "accept", "reject", "delete", "finger"), mcp.Description("Key management action: list, accept, reject, delete, finger")),
			mcp.WithString("match", mcp.Description("Minion ID or glob pattern for accept, reject, delete, or finger (e.g. 'web-01' or '*')")),
			mcp.WithBoolean("include_accepted", mcp.Description("When rejecting keys, also include accepted keys")),
			mcp.WithBoolean("include_rejected", mcp.Description("When accepting keys, also include rejected keys")),
			mcp.WithBoolean("include_denied", mcp.Description("When accepting or rejecting keys, also include denied keys")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			action, err := req.RequireString("action")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			match := strings.TrimSpace(req.GetString("match", ""))
			logger.Printf("Tool call: salt_keys_manage action=%s match=%s", action, match)

			client, _, err := buildClient(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			var res any
			switch action {
			case "list":
				res, err = client.ListKeys(ctx)
			case "accept":
				if match == "" {
					return mcp.NewToolResultError("match is required for action='accept'"), nil
				}
				res, err = client.AcceptKey(ctx, match, req.GetBool("include_rejected", false), req.GetBool("include_denied", false))
			case "reject":
				if match == "" {
					return mcp.NewToolResultError("match is required for action='reject'"), nil
				}
				res, err = client.RejectKey(ctx, match, req.GetBool("include_accepted", false), req.GetBool("include_denied", false))
			case "delete":
				if match == "" {
					return mcp.NewToolResultError("match is required for action='delete'"), nil
				}
				res, err = client.DeleteKey(ctx, match)
			case "finger":
				res, err = client.KeyFinger(ctx, match)
			default:
				return mcp.NewToolResultError(fmt.Sprintf("unsupported action %q", action)), nil
			}
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)

	// 12. salt_send_hook
	s.AddTool(
		mcp.NewTool("salt_send_hook",
			mcp.WithDescription("Trigger a Salt NetAPI webhook (POST /hook/<tag>) to fire Salt Master reactor events."),
			mcp.WithString("tag", mcp.Required(), mcp.Description("Webhook tag path (e.g. 'deploy/app')")),
			mcp.WithObject("payload", mcp.Description("JSON dictionary payload sent to the webhook")),
			mcp.WithString("profile", mcp.Description("Optional Salt Master profile name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tag, err := req.RequireString("tag")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			payload, _ := req.GetArguments()["payload"].(map[string]any)
			logger.Printf("Tool call: salt_send_hook tag=%s", tag)

			client, _, err := buildClient(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, err := client.SendWebhook(ctx, tag, payload)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonToolResult(res)
		},
	)
}

func jsonToolResult(data any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
