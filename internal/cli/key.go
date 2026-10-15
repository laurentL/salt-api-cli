package cli

import (
	"github.com/spf13/cobra"
)

func newKeyCmd(state *AppState) *cobra.Command {
	keyCmd := &cobra.Command{
		Use:     "key",
		Aliases: []string{"keys"},
		Short:   "Manage Salt minion keys via Master wheel (list, accept, reject, delete, finger)",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all minion keys grouped by status (wheel key.list_all)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			res, err := client.ListKeys(cmd.Context())
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}

	var incRejected, incDenied bool
	acceptCmd := &cobra.Command{
		Use:   "accept <match>",
		Short: "Accept pending minion key(s) matching glob/ID (wheel key.accept)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			res, err := client.AcceptKey(cmd.Context(), args[0], incRejected, incDenied)
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}
	acceptCmd.Flags().BoolVar(&incRejected, "include-rejected", false, "Also accept matching rejected keys")
	acceptCmd.Flags().BoolVar(&incDenied, "include-denied", false, "Also accept matching denied keys")

	var incAccepted, incRejDenied bool
	rejectCmd := &cobra.Command{
		Use:   "reject <match>",
		Short: "Reject minion key(s) matching glob/ID (wheel key.reject)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			res, err := client.RejectKey(cmd.Context(), args[0], incAccepted, incRejDenied)
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}
	rejectCmd.Flags().BoolVar(&incAccepted, "include-accepted", false, "Also reject matching accepted keys")
	rejectCmd.Flags().BoolVar(&incRejDenied, "include-denied", false, "Also reject matching denied keys")

	deleteCmd := &cobra.Command{
		Use:   "delete <match>",
		Short: "Delete minion key(s) matching glob/ID (wheel key.delete)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			res, err := client.DeleteKey(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}

	fingerCmd := &cobra.Command{
		Use:   "finger [match]",
		Short: "Display minion key fingerprints (wheel key.finger, default '*')",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			match := "*"
			if len(args) == 1 {
				match = args[0]
			}
			res, err := client.KeyFinger(cmd.Context(), match)
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}

	keyCmd.AddCommand(listCmd, acceptCmd, rejectCmd, deleteCmd, fingerCmd)
	return keyCmd
}
