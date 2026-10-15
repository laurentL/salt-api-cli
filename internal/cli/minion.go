package cli

import (
	"github.com/spf13/cobra"
)

func newMinionCmd(state *AppState) *cobra.Command {
	minionCmd := &cobra.Command{
		Use:     "minion",
		Aliases: []string{"minions"},
		Short:   "Inspect minions, grains inventory, and connectivity status",
	}

	var fullGrains bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List known minions and their summary inventory from /minions",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			minions, err := client.ListMinions(cmd.Context())
			if err != nil {
				return err
			}
			if fullGrains {
				return state.render(resolved, minions)
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
				if len(info) == 0 {
					summary[id] = "connected"
				} else {
					summary[id] = info
				}
			}
			return state.render(resolved, summary)
		},
	}
	listCmd.Flags().BoolVar(&fullGrains, "full", false, "Display complete grains dictionary for every minion")

	grainsCmd := &cobra.Command{
		Use:     "grains <minion_id> [grain_name...]",
		Aliases: []string{"get"},
		Short:   "Get all or specific grains for a minion (/minions/<id>)",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			minionID := args[0]
			grains, err := client.GetMinion(cmd.Context(), minionID)
			if err != nil {
				return err
			}

			if len(args) > 1 {
				if gm, ok := grains.(map[string]any); ok {
					filtered := make(map[string]any, len(args)-1)
					for _, k := range args[1:] {
						filtered[k] = gm[k]
					}
					return state.render(resolved, map[string]any{minionID: filtered})
				}
			}
			return state.render(resolved, map[string]any{minionID: grains})
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check up/down status of all minions via Master runner (manage.status)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			res, err := client.RunRunner(cmd.Context(), "manage.status", nil, nil)
			if err != nil {
				return err
			}
			return state.render(resolved, res)
		},
	}

	minionCmd.AddCommand(listCmd, grainsCmd, statusCmd)
	return minionCmd
}
