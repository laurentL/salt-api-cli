package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newJobCmd(state *AppState) *cobra.Command {
	jobCmd := &cobra.Command{
		Use:     "job",
		Aliases: []string{"jobs"},
		Short:   "Inspect and wait on Salt jobs (/jobs, /jobs/<jid>, jobs.active)",
	}

	var limit int
	var funFilter string

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent jobs from the Salt Master job cache (/jobs)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			jobs, err := client.ListJobs(cmd.Context())
			if err != nil {
				return err
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

			filtered := make(map[string]any, len(jids))
			for _, jid := range jids {
				filtered[jid] = jobs[jid]
			}
			return state.render(resolved, filtered)
		},
	}
	listCmd.Flags().IntVarP(&limit, "limit", "n", 25, "Maximum number of recent jobs to display (0 for all)")
	listCmd.Flags().StringVarP(&funFilter, "function", "f", "", "Filter jobs by function substring (e.g. state.apply)")

	var showInfo bool
	getCmd := &cobra.Command{
		Use:   "get <jid>",
		Short: "Get detailed return results for a specific Job ID (/jobs/<jid>)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			resp, err := client.GetJob(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if showInfo {
				out := map[string]any{
					"info":   resp.Info,
					"return": resp.Return,
				}
				return state.render(resolved, out)
			}
			if len(resp.Return) == 1 {
				return state.render(resolved, resp.Return[0])
			}
			return state.render(resolved, resp.Return)
		},
	}
	getCmd.Flags().BoolVar(&showInfo, "info", false, "Include full job metadata (Target, User, StartTime, Minions) in output")

	var interval time.Duration
	var waitTimeout time.Duration
	waitCmd := &cobra.Command{
		Use:   "wait <jid>",
		Short: "Wait for an asynchronous job to complete on all targeted minions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if waitTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, waitTimeout)
				defer cancel()
			}
			res, err := client.WaitJob(ctx, args[0], nil, interval)
			if err != nil {
				if res != nil {
					_ = state.render(resolved, res)
				}
				return err
			}
			return state.render(resolved, res)
		},
	}
	waitCmd.Flags().DurationVar(&interval, "interval", 1500*time.Millisecond, "Polling interval")
	waitCmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 5*time.Minute, "Maximum time to wait for job completion")

	activeCmd := &cobra.Command{
		Use:   "active",
		Short: "Show currently running jobs across all minions (runner jobs.active)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			res, err := client.RunRunner(cmd.Context(), "jobs.active", nil, nil)
			if err != nil {
				return fmt.Errorf("runner jobs.active failed: %w", err)
			}
			return state.render(resolved, res)
		},
	}

	jobCmd.AddCommand(listCmd, getCmd, waitCmd, activeCmd)
	return jobCmd
}
