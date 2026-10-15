package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/laurentL/salt-api-cli/internal/saltclient"
	"github.com/spf13/cobra"
)

func newRunCmd(state *AppState) *cobra.Command {
	tf := &TargetFlags{}

	cmd := &cobra.Command{
		Use:   "run <target> <function> [args...]",
		Short: "Execute a Salt module on targeted minions (local, local_async, or local_batch)",
		Example: `  sapi run '*' test.ping
  sapi '*' cmd.run 'df -h'
  sapi -G 'os_family:Debian' pkg.install vim refresh=True
  sapi run '*' state.apply common test=True
  sapi run --async --wait '*' pkg.upgrade`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeSaltRun(cmd.Context(), state, tf, args)
		},
	}

	tf.AddTargetFlags(cmd)
	return cmd
}

func newAsyncCmd(state *AppState) *cobra.Command {
	tf := &TargetFlags{}

	cmd := &cobra.Command{
		Use:   "async <target> <function> [args...]",
		Short: "Execute a Salt module asynchronously (local_async) and return JID (or --wait)",
		Example: `  sapi async '*' state.highstate
  sapi async --wait '*' state.highstate`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			tf.Async = true
			return executeSaltRun(cmd.Context(), state, tf, args)
		},
	}

	tf.AddTargetFlags(cmd)
	return cmd
}

func newBatchCmd(state *AppState) *cobra.Command {
	tf := &TargetFlags{}

	cmd := &cobra.Command{
		Use:   "batch <target> <function> [args...]",
		Short: "Execute a Salt module in progressive batches (local_batch)",
		Example: `  sapi batch -b 20% '*' state.apply
  sapi batch -b 5 -G 'role:web' service.restart nginx`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if tf.Batch == "" {
				tf.Batch = "10%"
			}
			return executeSaltRun(cmd.Context(), state, tf, args)
		},
	}

	tf.AddTargetFlags(cmd)
	return cmd
}

func executeSaltRun(ctx context.Context, state *AppState, tf *TargetFlags, args []string) error {
	client, resolved, err := state.buildClient(true)
	if err != nil {
		return err
	}

	tgt := args[0]
	fun := args[1]
	posArgs, kwargs := saltclient.ParseArgsAndKwargs(args[2:], tf.RawArgs)
	tgtType := tf.ResolveTargetType()

	if tf.Batch != "" {
		res, err := client.RunLocalBatch(ctx, tgt, fun, tf.Batch, posArgs, kwargs, tgtType)
		if err != nil {
			return err
		}
		return state.render(resolved, res)
	}

	if tf.Async || tf.Wait {
		asyncRes, err := client.RunLocalAsync(ctx, tgt, fun, posArgs, kwargs, tgtType, 0)
		if err != nil {
			return err
		}
		if !tf.Wait {
			out := map[string]any{
				"jid":     asyncRes.JID,
				"minions": asyncRes.Minions,
			}
			return state.render(resolved, out)
		}

		fmt.Fprintf(os.Stderr, "Started async job JID %s on %d minion(s), waiting for results...\n", asyncRes.JID, len(asyncRes.Minions))
		waitRes, err := client.WaitJob(ctx, asyncRes.JID, asyncRes.Minions, tf.WaitInterval)
		if err != nil {
			if waitRes != nil {
				_ = state.render(resolved, waitRes)
			}
			return err
		}
		return state.render(resolved, waitRes)
	}

	res, err := client.RunLocal(ctx, tgt, fun, posArgs, kwargs, tgtType, 0)
	if err != nil {
		return err
	}
	return state.render(resolved, res)
}
