package cli

import (
	"encoding/json"
	"fmt"

	"github.com/laurentL/salt-api-cli/internal/saltclient"
	"github.com/spf13/cobra"
)

func newEventCmd(state *AppState) *cobra.Command {
	eventCmd := &cobra.Command{
		Use:     "event",
		Aliases: []string{"events"},
		Short:   "Stream real-time Salt events (/events SSE) or trigger webhooks (/hook/<tag>)",
	}

	var tagPattern string
	var maxCount int
	listenCmd := &cobra.Command{
		Use:   "listen",
		Short: "Listen to real-time Salt Master event stream (/events SSE)",
		Example: `  sapi event listen
  sapi event listen --tag 'salt/job/*' --count 5 -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			return client.StreamEvents(cmd.Context(), tagPattern, maxCount, func(ev saltclient.Event) error {
				out := map[string]any{
					ev.Tag: ev.Data,
				}
				return state.render(resolved, out)
			})
		},
	}
	listenCmd.Flags().StringVar(&tagPattern, "tag", "", "Filter events by tag glob pattern (e.g. 'salt/job/*')")
	listenCmd.Flags().IntVarP(&maxCount, "count", "n", 0, "Stop after receiving N matching events (0 for infinite)")

	var dataJSON string
	hookCmd := &cobra.Command{
		Use:   "hook <tag> [key=value...]",
		Short: "Trigger a Salt NetAPI webhook (POST /hook/<tag>)",
		Example: `  sapi event hook deploy/app -d '{"version":"1.2.0","env":"prod"}'
  sapi event hook build/done status=success branch=main`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			payload := make(map[string]any)
			if dataJSON != "" {
				if err := json.Unmarshal([]byte(dataJSON), &payload); err != nil {
					return fmt.Errorf("invalid --data JSON payload: %w", err)
				}
			}
			_, kw := saltclient.ParseArgsAndKwargs(args[1:], false)
			for k, v := range kw {
				payload[k] = v
			}

			res, err := client.SendWebhook(cmd.Context(), args[0], payload)
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}
	hookCmd.Flags().StringVarP(&dataJSON, "data", "d", "", "JSON object payload to send to the webhook")

	eventCmd.AddCommand(listenCmd, hookCmd)
	return eventCmd
}
