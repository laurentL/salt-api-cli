package cli

import (
	"github.com/laurentL/salt-api-cli/internal/saltclient"
	"github.com/spf13/cobra"
)

func newRunnerCmd(state *AppState) *cobra.Command {
	var async bool
	var rawArgs bool

	cmd := &cobra.Command{
		Use:   "runner <function> [args...]",
		Short: "Execute a Salt Master runner module (client=runner or runner_async)",
		Example: `  sapi runner manage.status
  sapi runner jobs.active
  sapi runner state.orchestrate orch.deploy pillar='{"env":"prod"}'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			fun := args[0]
			posArgs, kwargs := saltclient.ParseArgsAndKwargs(args[1:], rawArgs)

			if async {
				res, err := client.RunRunnerAsync(cmd.Context(), fun, posArgs, kwargs)
				if err != nil {
					return err
				}
				return state.render(resolved, map[string]any{"jid": res.JID, "tag": res.Tag})
			}

			res, err := client.RunRunner(cmd.Context(), fun, posArgs, kwargs)
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}

	cmd.Flags().BoolVar(&async, "async", false, "Run asynchronously via runner_async")
	cmd.Flags().BoolVar(&rawArgs, "raw-args", false, "Pass all arguments as raw strings")
	return cmd
}

func newWheelCmd(state *AppState) *cobra.Command {
	var async bool
	var rawArgs bool

	cmd := &cobra.Command{
		Use:   "wheel <function> [args...]",
		Short: "Execute a Salt Master wheel function (client=wheel or wheel_async)",
		Example: `  sapi wheel key.list_all
  sapi wheel config.values`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			fun := args[0]
			posArgs, kwargs := saltclient.ParseArgsAndKwargs(args[1:], rawArgs)

			if async {
				res, err := client.RunWheelAsync(cmd.Context(), fun, posArgs, kwargs)
				if err != nil {
					return err
				}
				return state.render(resolved, map[string]any{"jid": res.JID, "tag": res.Tag})
			}

			res, err := client.RunWheel(cmd.Context(), fun, posArgs, kwargs)
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}

	cmd.Flags().BoolVar(&async, "async", false, "Run asynchronously via wheel_async")
	cmd.Flags().BoolVar(&rawArgs, "raw-args", false, "Pass all arguments as raw strings")
	return cmd
}

func newStatsCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Retrieve salt-api server statistics (/stats)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			res, err := client.Stats(cmd.Context())
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}
}
